// Package sync 提供 npm 镜像的同步调度与状态管理能力，强调“为什么做”的业务约束：
// - 解耦为管理、计划、上传、对账、状态、缓存多个模块，降低单文件复杂度与变更风险；
// - Pull/BootSync/StartCron 分别覆盖手动同步、启动对账、周期任务三种场景，便于扩展与观测；
// - 外部依赖通过注入保持可选性（数据源/对象存储/数据库/缓存），避免强耦合。
package sync

import (
    "context"
    "fmt"
    "strings"
    "sync"
    "time"

    "npm-mirror/config"
    "npm-mirror/internal/datasource"
    "npm-mirror/internal/logging"
    "npm-mirror/internal/models"
    "npm-mirror/internal/s3client"
    pgstore "npm-mirror/internal/storage/postgres"
)

// SyncManager 同步管理器：对外提供统一的同步入口与状态查询。
// 为什么：多触发/观测路径（API/UI/定时器）集中治理，便于审计与演进。
type SyncManager struct {
    config     *config.Config
    s3Client   *s3client.Client
    syncState  *models.SyncState
    stateMutex sync.RWMutex
    store      *pgstore.Store
    ds         *datasource.Source
    logger     *logging.Logger
    rcache     interface{ Set(context.Context, string, string) error; SetMany(context.Context, map[string]string) error }
    jobsMu     sync.Mutex
    jobs       map[string]bool
}

// NewSyncManager 构造函数：通过依赖注入保持灵活组合，避免硬编码实现。
func NewSyncManager(cfg *config.Config, s3Client *s3client.Client) *SyncManager {
    return &SyncManager{
        config:   cfg,
        s3Client: s3Client,
        syncState: &models.SyncState{
            PackageStates: make(map[string]models.PackageState),
        },
    }
}

// SetStore 设置持久化存储（可选）。
// 为什么：入库便于统计与对账；不可用时退化为仅S3与内存态。
func (sm *SyncManager) SetStore(store *pgstore.Store) { sm.store = store }
// SetCaches 配置缓存写入能力（Redis/LRU等，可选）。
// 为什么：成功同步后更新热点键提升查询体验；失败不写避免脏读。
func (sm *SyncManager) SetCaches(rc interface{ Set(context.Context, string, string) error; SetMany(context.Context, map[string]string) error }) {
    sm.rcache = rc
}
// SetDataSource 注入数据源客户端（HTTP/快照/自定义）。保持来源可替换。
func (sm *SyncManager) SetDataSource(ds *datasource.Source) { sm.ds = ds }
// SetLogger 注入日志器，用于审计与定位问题。
func (sm *SyncManager) SetLogger(l *logging.Logger) { sm.logger = l }

func (sm *SyncManager) TryStartJob(name string) bool {
    sm.jobsMu.Lock()
    defer sm.jobsMu.Unlock()
    if sm.jobs == nil {
        sm.jobs = make(map[string]bool)
    }
    if sm.jobs[name] {
        return false
    }
    sm.jobs[name] = true
    return true
}

func (sm *SyncManager) FinishJob(name string) {
    sm.jobsMu.Lock()
    if sm.jobs != nil {
        delete(sm.jobs, name)
    }
    sm.jobsMu.Unlock()
}

func (sm *SyncManager) IsJobRunning(name string) bool {
    sm.jobsMu.Lock()
    defer sm.jobsMu.Unlock()
    if sm.jobs == nil {
        return false
    }
    return sm.jobs[name]
}

// GetSyncState 获取当前同步状态快照（读锁保护）。
// 为什么：供 API/UI 查询，不暴露内部并发细节。
func (sm *SyncManager) GetSyncState() *models.SyncState {
    sm.stateMutex.RLock()
    defer sm.stateMutex.RUnlock()
    return sm.syncState
}

// Pull 执行一次同步：拉取数据源 → 计划优先级 → 并发上传 → 状态持久化 → 失败重试。
// 为什么：单一流程覆盖手动与定时触发，确保一致的状态与可追踪失败。
// NOTE: 回填批次受配置约束，避免历史版本过多导致阻塞。
func (sm *SyncManager) Pull(ctx context.Context) error {
    if sm.logger != nil {
        sm.logger.Info(
            "sync",
            "开始拉取数据源",
            map[string]interface{}{
                "url": sm.config.DataSourceURL,
            },
        )
    } else {
        fmt.Printf("开始拉取数据源: %s\n", sm.config.DataSourceURL)
    }

    var data models.DataSource
    if sm.ds != nil {
        d, err := sm.ds.Get(ctx)
        if err != nil { return fmt.Errorf("拉取数据源失败: %v", err) }
        data = d
    } else {
        return fmt.Errorf("数据源未配置")
    }

    if sm.logger != nil {
        sm.logger.Info(
            "sync",
            "发现数据源包",
            map[string]interface{}{
                "total": data.Total,
            },
        )
    } else {
        fmt.Printf("发现 %d 个包\n", data.Total)
    }

    sm.stateMutex.Lock()
    sm.syncState.LastSyncTime = time.Now()
    sm.syncState.TotalPackages = data.Total
    sm.syncState.SyncedPackages = 0
    sm.syncState.FailedPackages = 0
    sm.stateMutex.Unlock()

    _ = sm.loadSyncState(ctx)

    packagesToSync := sm.determinePackagesToSync(data.Packages)
    if sm.logger != nil {
        sm.logger.Info(
            "sync",
            "需要同步包",
            map[string]interface{}{
                "count": len(packagesToSync),
            },
        )
    } else {
        fmt.Printf("需要同步 %d 个包\n", len(packagesToSync))
    }

    pri, back := sm.planPriorityAndBackfill(packagesToSync)
    if sm.config.BackfillEnabled && sm.config.BackfillBatch > 0 && len(back) > sm.config.BackfillBatch {
        back = back[:sm.config.BackfillBatch]
    }

    sem := make(chan struct{}, sm.config.Concurrency)
    var wg sync.WaitGroup
    var syncErrMutex sync.Mutex
    var syncErrors []error

    schedule := func(list []models.Package) {
        for _, pkg := range list {
            wg.Add(1)
            sem <- struct{}{}
            go func(p models.Package) {
                defer wg.Done()
                defer func() { <-sem }()
                if err := sm.syncPackage(ctx, p); err != nil {
                    syncErrMutex.Lock()
                    syncErrors = append(syncErrors, fmt.Errorf("同步包 %s@%s 失败: %v", p.Name, p.Version, err))
                    syncErrMutex.Unlock()
                }
            }(pkg)
        }
    }

    schedule(pri)
    if sm.config.BackfillEnabled { schedule(back) }

    wg.Wait()
    _ = sm.saveSyncState(ctx)
    sm.retryFailedPackages(ctx)

    if len(syncErrors) > 0 {
        return fmt.Errorf("同步完成，但有 %d 个包同步失败: %v", len(syncErrors), syncErrors[0])
    }
    if sm.logger != nil {
        sm.logger.Info("sync", "所有包同步完成", nil)
    } else {
        fmt.Println("所有包同步完成！")
    }
    return nil
}

// BootSync 启动阶段的对账与缺失同步：清理临时缓存、加载状态、对账并补齐缺失对象。
// 为什么：减少“已有对象但DB缺失”的不一致，提升查询与展示的可靠性。
func (sm *SyncManager) BootSync(ctx context.Context) error {
    sm.cleanCaches(ctx)
    _ = sm.loadSyncState(ctx)
    recon, _ := sm.Reconcile(ctx)

    if sm.ds == nil { return fmt.Errorf("数据源未配置") }
    d, err := sm.ds.Get(ctx)
    if err != nil { return err }
    data := d

    s3idx, _ := sm.buildS3Index(ctx)

    var toSync []models.Package
    for _, pkg := range data.Packages {
        key := s3client.NormalizePackageName(pkg.Name) + "@" + pkg.Version
        if _, ok := s3idx[key]; !ok { toSync = append(toSync, pkg) }
    }

    if sm.isDebug() { sm.printInitSummary(ctx, data.Total, len(s3idx), recon, toSync) }
    if len(toSync) == 0 { return nil }

    sem := make(chan struct{}, sm.config.Concurrency)
    var wg sync.WaitGroup
    for _, p := range toSync {
        wg.Add(1)
        sem <- struct{}{}
        go func(pkg models.Package) {
            defer wg.Done()
            defer func() { <-sem }()
            _ = sm.syncPackage(ctx, pkg)
        }(p)
    }
    wg.Wait()
    _ = sm.saveSyncState(ctx)
    return nil
}

// isDebug 判断是否处于调试日志级别，用于输出启动摘要。
func (sm *SyncManager) isDebug() bool {
    lv := strings.ToLower(sm.config.LogLevel)
    return lv == "debug" || lv == "trace"
}

// printInitSummary 打印启动阶段的关键指标与差异摘要，便于观察系统基线。
// 为什么：冷启动或迁移时快速感知体量与差异，辅助评估与排障。
func (sm *SyncManager) printInitSummary(ctx context.Context, dsTotal, s3Count int, recon *ReconcileSummary, toSync []models.Package) {
    var pkgs, versions int
    var status map[string]int
    var latest time.Time
    if sm.store != nil {
        pkgs, _ = sm.store.CountDistinctPackages(ctx)
        versions, _ = sm.store.CountVersions(ctx)
        status, _ = sm.store.StatusCounts(ctx)
        latest, _ = sm.store.LatestSyncTime(ctx)
    }
    fmt.Printf("\n启动初始化摘要\n")
    fmt.Printf("数据库: 包=%d 版本=%d 状态: success=%d failed=%d pending=%d syncing=%d 最新同步=%s\n",
        pkgs,
        versions,
        status["success"],
        status["failed"],
        status["pending"],
        status["syncing"],
        latest.Format("2006-01-02 15:04:05"),
    )
    var dbMissing, s3Missing, mismatched, planned int
    if recon != nil {
        dbMissing = len(recon.DBMissingS3)
        s3Missing = len(recon.S3MissingDB)
        mismatched = len(recon.SizeMismatched)
        planned = len(recon.ScheduledResync)
    }
    fmt.Printf(
        "对象存储: tgz对象=%d DB缺失S3=%d S3缺失DB=%d 大小不一致=%d\n",
        s3Count,
        dbMissing,
        s3Missing,
        mismatched,
    )
    fmt.Printf("数据源: 总包=%d 首次待同步=%d 计划重试=%d\n\n", dsTotal, len(toSync), planned)
}

// StartCron 周期触发同步任务：定时清理临时缓存并执行 Pull。
// 为什么：保持与数据源持续收敛；ticker 生命周期确保 Stop 后资源回收。
func (sm *SyncManager) StartCron(ctx context.Context) {
    sm.cleanCaches(ctx)
    if err := sm.Pull(ctx); err != nil { fmt.Printf("首次同步失败: %v\n", err) }

    ticker := time.NewTicker(sm.config.SyncInterval)
    defer ticker.Stop()

    for {
        select {
        case <-ticker.C:
            fmt.Printf("\n定时同步触发（%s）\n", time.Now().Format("2006-01-02 15:04:05"))
            sm.cleanCaches(ctx)
            if err := sm.Pull(ctx); err != nil { fmt.Printf("定时同步失败: %v\n", err) }
        case <-ctx.Done():
            fmt.Println("定时同步任务已停止")
            return
        }
    }
}

// RetryFailedNow 立即触发失败重试（与内部重试策略一致）。
// 为什么：运营或排障场景下，外部介入后快速尝试恢复失败项。
func (sm *SyncManager) RetryFailedNow(ctx context.Context) { sm.retryFailedPackages(ctx) }
