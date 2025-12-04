package main

import (
    "context"
    "fmt"
    "log"
    "net/http"
    "net"
    "os"
    "os/signal"
    "syscall"
    "time"

    "github.com/gin-gonic/gin"
    "npm-mirror/config"
    "npm-mirror/internal/api"
    "npm-mirror/internal/s3client"
    "npm-mirror/internal/sync"
    pgstore "npm-mirror/internal/storage/postgres"
    "npm-mirror/internal/cache"
    "npm-mirror/internal/datasource"
    "npm-mirror/internal/logging"
)

func main() {
	// 加载配置
	cfg := config.LoadConfig()

	// 初始化S3客户端
	s3Client, err := s3client.New(cfg)
	if err != nil {
		log.Fatalf("初始化S3客户端失败: %v", err)
	}

    // 初始化同步管理器
    syncManager := sync.NewSyncManager(cfg, s3Client)

	// 初始化Postgres（可选）
	var store *pgstore.Store
	// 优先使用 DB_DSN，如果未设置则回退到多字段配置
	dsn := os.Getenv("DB_DSN")
	if dsn == "" && cfg.PGHost != "" && cfg.PGUser != "" && cfg.PGDB != "" {
		dsn = fmt.Sprintf("postgres://%s:%s@%s:%s/%s?sslmode=%s", cfg.PGUser, cfg.PGPassword, cfg.PGHost, cfg.PGPort, cfg.PGDB, cfg.PGSSLMode)
	}

	if dsn != "" {
		s, err := pgstore.New(context.Background(), dsn)
		if err != nil {
			log.Fatalf("初始化Postgres失败: %v", err)
		}
		if err := s.EnsureSchema(context.Background()); err != nil {
			log.Fatalf("创建/检查数据库表失败: %v", err)
		}
		store = s
		syncManager.SetStore(store)
	}

    // 初始化缓存与数据源
    rd := cache.NewRedis(cfg.RedisAddr, cfg.RedisPassword, cfg.RedisDB, cfg.CacheTTL)
    ds := datasource.New(cfg, rd)
    syncManager.SetDataSource(ds)

    // 初始化日志
    logger := logging.New(cfg.LogLevel, store)
    syncManager.SetLogger(logger)
    ds.SetLogger(logger)
    s3Client.SetLogger(logger)
    if store != nil { _ = store.PurgeLogsOlderThan(context.Background(), 30) }
    if store != nil {
        logger.Info("health", "db_log_test_boot", map[string]interface{}{"ok": true})
    }

	// 初始化Gin路由
	gin.SetMode(gin.ReleaseMode)
	r := gin.Default()

	// 添加CORS中间件
	r.Use(func(c *gin.Context) {
		c.Writer.Header().Set("Access-Control-Allow-Origin", "*")
		c.Writer.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		c.Writer.Header().Set("Access-Control-Allow-Headers", "Origin, Content-Type, Content-Length, Accept-Encoding, X-CSRF-Token, Authorization")

		if c.Request.Method == "OPTIONS" {
			c.AbortWithStatus(http.StatusNoContent)
			return
		}

		c.Next()
	})

	// 注册路由
    handler := api.NewHandler(cfg, s3Client, syncManager, store, ds)
	handler.RegisterRoutes(r)

	// 创建HTTP服务器
	srv := &http.Server{
		Addr:    ":" + cfg.APIPort,
		Handler: r,
	}

    ctx, cancel := context.WithCancel(context.Background())
    defer cancel()

    ln, err := net.Listen("tcp", ":"+cfg.APIPort)
    if err != nil {
        log.Fatalf("监听端口失败: %v", err)
    }
    fmt.Printf("服务启动成功，监听端口: %s\n", cfg.APIPort)
    go func() {
        if err := srv.Serve(ln); err != nil && err != http.ErrServerClosed {
            log.Fatalf("服务启动失败: %v", err)
        }
    }()

    go func() {
        start := time.Now()
        if err := syncManager.BootSync(ctx); err != nil {
            logger.Error("boot", "首次同步失败", map[string]interface{}{"error": err.Error()})
        } else {
            logger.Info("boot", "首次同步完成", map[string]interface{}{"seconds": fmt.Sprintf("%.2f", time.Since(start).Seconds())})
        }
        syncManager.StartCron(ctx)
    }()

	// 等待中断信号，优雅关闭
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	fmt.Println("服务正在关闭...")

	// 取消定时任务
	cancel()

	// 关闭HTTP服务
	ctx, cancel = context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		log.Fatalf("服务关闭失败: %v", err)
	}

	fmt.Println("服务已关闭")
}
