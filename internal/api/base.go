package api

import (
    "npm-mirror/config"
    cache "npm-mirror/internal/cache"
    "npm-mirror/internal/datasource"
    "npm-mirror/internal/proxy"
    "npm-mirror/internal/s3client"
    pgstore "npm-mirror/internal/storage/postgres"
    "npm-mirror/internal/sync"
    "npm-mirror/internal/version"
)

// NOTE: 领域解耦：基础 Handler 与依赖初始化集中此处，其他业务按模块拆分到独立文件。
type Handler struct {
    config      *config.Config
    s3Client    *s3client.Client
    syncManager *sync.SyncManager
    vc          *version.Controller
    ds          *datasource.Source
    store       *pgstore.Store
    upstream    *proxy.UpstreamProxy
}

func NewHandler(cfg *config.Config, s3Client *s3client.Client, syncManager *sync.SyncManager, store *pgstore.Store, ds *datasource.Source) *Handler {
    rcache := cache.NewRedis(cfg.RedisAddr, cfg.RedisPassword, cfg.RedisDB, cfg.CacheTTL)
    var lru *cache.LRU
    if cfg.LocalCacheEnabled { lru = cache.NewLRU(cfg.LocalCacheSize, cfg.CacheTTL) }
    if rcache != nil { syncManager.SetCaches(rcache) }
    return &Handler{
        config:      cfg,
        s3Client:    s3Client,
        syncManager: syncManager,
        vc:          version.NewController(cfg, s3Client, syncManager, store, rcache, lru),
        ds:          ds,
        store:       store,
        upstream:    proxy.NewUpstreamProxy(""),
    }
}

