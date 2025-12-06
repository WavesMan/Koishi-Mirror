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
	// TENCENT_COS 配置
	TencentCosendpoint  string
	TencentCosaccesskey string
	TencentCossecretkey string
	TencentCosregion    string
	TencentCosbucket    string
	TencentCosprefix    string

	// CDN 配置
	CDNEndpoint string
	CDNEnabled  bool

	// 回填配置
	BackfillEnabled bool
	BackfillBatch   int

	LocalCacheEnabled bool
	LocalCacheSize    int
	CacheSoftTTL      time.Duration

	// 同步配置
	DataSourceURL        string
	SyncInterval         time.Duration
	Concurrency          int
	MaxRetries           int
	DataSourceTimeout    time.Duration
	DataSourceMaxRetries int
	LogLevel             string

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

    // ICP 配置
    ICPEnabled     bool
    ICPRecord      string
    ICPUrl         string
    SecurityRecord string
    SecurityUrl    string

    // Server 超时与头部限制
    ServerReadTimeout  time.Duration
    ServerWriteTimeout time.Duration
    ServerIdleTimeout  time.Duration
    MaxHeaderBytes     int

    // 限流配置
    RLEnabled    bool
    RLGlobalQPS  int
    RLPerIPQPS   int
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
	cdnEnabled := strings.EqualFold(getEnv("CDN_ENABLED", "false"), "true")
	if v := getEnv("CDNEnabled", ""); v != "" {
		cdnEnabled = strings.EqualFold(v, "true")
	}
	cdnEndpoint := getEnv("CDN_ENDPOINT", "")
	if v := getEnv("CDNEndpoint", ""); v != "" {
		cdnEndpoint = v
	}

	backfillEnabled := strings.EqualFold(getEnv("BACKFILL_ENABLED", "false"), "true")
	if v := getEnv("BackfillEnabled", ""); v != "" {
		backfillEnabled = strings.EqualFold(v, "true")
	}
	backfillBatch := 50
	if v := getEnv("BACKFILL_BATCH", ""); v != "" {
		if n, e := strconv.Atoi(v); e == nil {
			backfillBatch = n
		}
	}
	if v := getEnv("BackfillBatch", ""); v != "" {
		if n, e := strconv.Atoi(v); e == nil {
			backfillBatch = n
		}
	}

	localCacheEnabled := strings.EqualFold(getEnv("LOCAL_CACHE_ENABLED", "false"), "true")
	if v := getEnv("LocalCacheEnabled", ""); v != "" {
		localCacheEnabled = strings.EqualFold(v, "true")
	}
	localCacheSize := 10000
	if v := getEnv("LOCAL_CACHE_SIZE", ""); v != "" {
		if n, e := strconv.Atoi(v); e == nil {
			localCacheSize = n
		}
	}
	if v := getEnv("LocalCacheSize", ""); v != "" {
		if n, e := strconv.Atoi(v); e == nil {
			localCacheSize = n
		}
	}

	cacheSoftTTL, _ := time.ParseDuration(getEnv("CACHE_SOFT_TTL", "5m"))
	if v := getEnv("CacheSoftTTL", ""); v != "" {
		if d, e := time.ParseDuration(v); e == nil {
			cacheSoftTTL = d
		}
	}

	icpEnabled := strings.EqualFold(getEnv("ICP_ENABLED", "false"), "true")
	if v := getEnv("ICPEnabled", ""); v != "" {
		icpEnabled = strings.EqualFold(v, "true")
	}
	icpRecord := getEnv("ICP_RECORD", "")
	if v := getEnv("ICPRecord", ""); v != "" {
		icpRecord = v
	}
	icpUrl := getEnv("ICP_URL", "")
	if v := getEnv("ICPUrl", ""); v != "" {
		icpUrl = v
	}
	securityRecord := getEnv("SECURITY_RECORD", "")
	if v := getEnv("SecurityRecord", ""); v != "" {
		securityRecord = v
	}
    securityUrl := getEnv("SECURITY_URL", "")
    if v := getEnv("SecurityUrl", ""); v != "" {
        securityUrl = v
    }

    readTimeout, _ := time.ParseDuration(getEnv("READ_TIMEOUT", "10s"))
    writeTimeout, _ := time.ParseDuration(getEnv("WRITE_TIMEOUT", "20s"))
    idleTimeout, _ := time.ParseDuration(getEnv("IDLE_TIMEOUT", "60s"))
    maxHeaderBytes, _ := strconv.Atoi(getEnv("MAX_HEADER_BYTES", "1048576"))

    rlEnabled := strings.EqualFold(getEnv("RL_ENABLED", "false"), "true")
    rlGlobalQPS, _ := strconv.Atoi(getEnv("RL_GLOBAL_QPS", "0"))
    rlPerIPQPS, _ := strconv.Atoi(getEnv("RL_PER_IP_QPS", "0"))

    return &Config{
		// TENCENT_COS 配置
		TencentCosendpoint:  getEnv("TENCENT_COS_ENDPOINT", ""),
		TencentCosaccesskey: getEnv("TENCENT_COS_ACCESS_KEY", ""),
		TencentCossecretkey: getEnv("TENCENT_COS_SECRET_KEY", ""),
		TencentCosregion:    getEnv("TENCENT_COS_REGION", "ap_shanghai"),
		TencentCosbucket:    getEnv("TENCENT_COS_BUCKET", "waveyo-koishi-mirror"),
		TencentCosprefix:    getEnv("TENCENT_COS_PREFIX", "packages/"),

		CDNEndpoint:       cdnEndpoint,
		CDNEnabled:        cdnEnabled,
		BackfillEnabled:   backfillEnabled,
		BackfillBatch:     backfillBatch,
		LocalCacheEnabled: localCacheEnabled,
		LocalCacheSize:    localCacheSize,
		CacheSoftTTL:      cacheSoftTTL,

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

		// ICP 配置
		ICPEnabled:     icpEnabled,
		ICPRecord:      icpRecord,
		ICPUrl:         icpUrl,
        SecurityRecord: securityRecord,
        SecurityUrl:    securityUrl,

        ServerReadTimeout:  readTimeout,
        ServerWriteTimeout: writeTimeout,
        ServerIdleTimeout:  idleTimeout,
        MaxHeaderBytes:     maxHeaderBytes,

        RLEnabled:   rlEnabled,
        RLGlobalQPS: rlGlobalQPS,
        RLPerIPQPS:  rlPerIPQPS,
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
            _ = os.Setenv(key, val)
        }
        _ = f.Close()
        break
    }
}
