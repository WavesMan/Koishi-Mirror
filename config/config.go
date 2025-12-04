package config

import (
    "bufio"
    "os"
    "strconv"
    "strings"
    "time"
)

// Config 应用配置
type Config struct {
	// S3 配置
	S3Endpoint  string
	S3AccessKey string
	S3SecretKey string
	S3Region    string
	S3Bucket    string
	S3Prefix    string

    // 同步配置
    DataSourceURL string
    SyncInterval  time.Duration
    Concurrency   int
    MaxRetries    int
    DataSourceTimeout    time.Duration
    DataSourceMaxRetries int
    LogLevel string

	// API 配置
	APIPort string

    // 缓存配置
    RedisAddr     string
    RedisPassword string
    RedisDB       int
    CacheTTL      time.Duration

	// 数据库配置
	PGHost     string
	PGPort     string
	PGUser     string
	PGPassword string
	PGDB       string
	PGSSLMode  string
}

// LoadConfig 从环境变量加载配置
func LoadConfig() *Config {
	loadDotEnv()
	concurrency, _ := strconv.Atoi(getEnv("SYNC_CONCURRENCY", "10"))
	maxRetries, _ := strconv.Atoi(getEnv("SYNC_MAX_RETRIES", "3"))
	syncInterval, _ := time.ParseDuration(getEnv("SYNC_INTERVAL", "1h"))
	redisDB, _ := strconv.Atoi(getEnv("REDIS_DB", "0"))
	cacheTTL, _ := time.ParseDuration(getEnv("CACHE_TTL", "10m"))
	dsTimeout, _ := time.ParseDuration(getEnv("DATA_SOURCE_TIMEOUT", "15s"))
	dsMaxRetries, _ := strconv.Atoi(getEnv("DATA_SOURCE_MAX_RETRIES", "3"))
	logLevel := getEnv("LOG_LEVEL", "info")
	if v := getEnv("LogLevel", ""); v != "" {
		logLevel = v
	}

	return &Config{
		// S3 配置
		S3Endpoint:  getEnv("S3_ENDPOINT", ""),
		S3AccessKey: getEnv("S3_ACCESS_KEY", ""),
		S3SecretKey: getEnv("S3_SECRET_KEY", ""),
		S3Region:    getEnv("S3_REGION", "us-east-1"),
		S3Bucket:    getEnv("S3_BUCKET", "waveyo-npm-mirror"),
		S3Prefix:    getEnv("S3_PREFIX", "packages/"),

		// 同步配置
		DataSourceURL:        getEnv("DATA_SOURCE_URL", "https://ks-store.waveyo.cn/index.json"),
		SyncInterval:         syncInterval,
		Concurrency:          concurrency,
		MaxRetries:           maxRetries,
		DataSourceTimeout:    dsTimeout,
		DataSourceMaxRetries: dsMaxRetries,
		LogLevel:             logLevel,

		// API 配置
		APIPort: getEnv("API_PORT", "8080"),

		// 缓存配置
		RedisAddr:     getEnv("REDIS_ADDR", ""),
		RedisPassword: getEnv("REDIS_PASSWORD", ""),
		RedisDB:       redisDB,
		CacheTTL:      cacheTTL,

		// 数据库配置
		PGHost:     getEnv("PG_HOST", ""),
		PGPort:     getEnv("PG_PORT", "5432"),
		PGUser:     getEnv("PG_USER", ""),
		PGPassword: getEnv("PG_PASSWORD", ""),
		PGDB:       getEnv("PG_DB", ""),
		PGSSLMode:  getEnv("PG_SSLMODE", "disable"),
	}
}

// getEnv 获取环境变量，如果不存在则返回默认值
func getEnv(key, defaultValue string) string {
    value := os.Getenv(key)
    if value == "" {
        return defaultValue
    }
    return value
}

func loadDotEnv() {
    paths := []string{".env", ".env.example"}
    for _, p := range paths {
        f, err := os.Open(p)
        if err != nil {
            continue
        }
        scanner := bufio.NewScanner(f)
        for scanner.Scan() {
            line := strings.TrimSpace(scanner.Text())
            if line == "" || strings.HasPrefix(line, "#") {
                continue
            }
            idx := strings.Index(line, "=")
            if idx <= 0 {
                continue
            }
            key := strings.TrimSpace(line[:idx])
            val := strings.TrimSpace(line[idx+1:])
            os.Setenv(key, val)
        }
        f.Close()
        break
    }
}
