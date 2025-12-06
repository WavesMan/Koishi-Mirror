package sync

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"sync"
	"time"

	"npm-mirror/internal/models"
)

// updatePackageState 更新内存态并刷新统计。
// 为什么：统一管理包级状态与重试计数，提供稳定的查询视图。
func (sm *SyncManager) updatePackageState(pkgKey, status string, size int64) {
	sm.stateMutex.Lock()
	defer sm.stateMutex.Unlock()

	state, exists := sm.syncState.PackageStates[pkgKey]
	if !exists {
		state = models.PackageState{}
	}

	state.SyncStatus = status
	state.SyncTime = time.Now()
	if status == "failed" {
		state.RetryCount++
	}
	if status == "success" {
		state.Size = size
	}

	sm.syncState.PackageStates[pkgKey] = state
	sm.updateSyncStats()
}

// updateSyncStats 刷新统计聚合（成功/失败）。
// 为什么：供 UI/API 展示总体进度，避免每次遍历计算。
func (sm *SyncManager) updateSyncStats() {
	synced := 0
	failed := 0
	for _, state := range sm.syncState.PackageStates {
		if state.SyncStatus == "success" {
			synced++
		} else if state.SyncStatus == "failed" {
			failed++
		}
	}
	sm.syncState.SyncedPackages = synced
	sm.syncState.FailedPackages = failed
}

// retryFailedPackages 并发重试失败包（受重试上限与并发度约束）。
// 为什么：在网络或上游短暂故障后，快速收敛失败项，提升总体完成度。
// TODO: 可引入指数退避与错误分类策略，减少雪崩与无效重试。
func (sm *SyncManager) retryFailedPackages(ctx context.Context) {
	sm.stateMutex.RLock()
	var failedPackages []models.Package
	for pkgKey, state := range sm.syncState.PackageStates {
		if state.SyncStatus == "failed" && state.RetryCount < sm.config.MaxRetries {
			var name, version string
			_, _ = fmt.Sscanf(pkgKey, "%s@%s", &name, &version)
			pkg := models.Package{Name: name, Version: version}
			failedPackages = append(failedPackages, pkg)
		}
	}
	sm.stateMutex.RUnlock()
	if len(failedPackages) == 0 {
		return
	}

	fmt.Printf("开始重试 %d 个失败的包，最大重试次数: %d\n", len(failedPackages), sm.config.MaxRetries)
	sem := make(chan struct{}, sm.config.Concurrency)
	var wg sync.WaitGroup
	for _, pkg := range failedPackages {
		wg.Add(1)
		sem <- struct{}{}
		go func(p models.Package) {
			defer wg.Done()
			defer func() { <-sem }()
			fmt.Printf("重试同步: %s@%s\n", p.Name, p.Version)
			if err := sm.fetchPackageInfo(ctx, &p); err != nil {
				fmt.Printf("获取包信息失败 %s@%s: %v\n", p.Name, p.Version, err)
				return
			}
			if err := sm.syncPackage(ctx, p); err != nil {
				fmt.Printf("重试失败 %s@%s: %v\n", p.Name, p.Version, err)
			} else {
				fmt.Printf("重试成功 %s@%s\n", p.Name, p.Version)
			}
		}(pkg)
	}
	wg.Wait()
	if err := sm.saveSyncState(ctx); err != nil {
		fmt.Printf("保存同步状态失败: %v\n", err)
	}
}

// fetchPackageInfo 从数据源补全包的完整信息。
// 为什么：失败重试时需要最新的元数据与下载地址，避免使用过期信息。
func (sm *SyncManager) fetchPackageInfo(ctx context.Context, pkg *models.Package) error {
	if sm.ds == nil {
		return fmt.Errorf("数据源未配置")
	}
	d, err := sm.ds.Get(ctx)
	if err != nil {
		return err
	}
	for _, p := range d.Packages {
		if p.Name == pkg.Name && p.Version == pkg.Version {
			*pkg = p
			return nil
		}
	}
	return fmt.Errorf("包信息不存在")
}

// savePackageMetadata 将包元数据写入对象存储。
// 为什么：提供无需回源即可构建页面/API 的材料，降低后续查询成本。
func (sm *SyncManager) savePackageMetadata(ctx context.Context, pkg models.Package) error {
	metadataJSON, err := json.Marshal(pkg)
	if err != nil {
		return fmt.Errorf("序列化元数据失败: %v", err)
	}
	metaKey := sm.s3Client.GetPackageMetaKey(pkg.Name)
	return sm.s3Client.UploadFile(ctx, metaKey, bytes.NewReader(metadataJSON), "application/json", int64(len(metadataJSON)))
}

// loadSyncState 加载同步状态（优先DB，否则S3）。
// 为什么：支持不同部署环境的能力差异，尽可能维持有状态的同步视图。
func (sm *SyncManager) loadSyncState(ctx context.Context) error {
	if sm.store != nil {
		st, err := sm.store.LoadSyncState(ctx)
		if err != nil {
			return err
		}
		sm.stateMutex.Lock()
		sm.syncState = st
		sm.stateMutex.Unlock()
		return nil
	}
	key := sm.s3Client.GetSyncStateKey()
	r, err := sm.s3Client.DownloadFile(ctx, key)
	if err != nil {
		return err
	}
	defer func(r io.ReadCloser) {
		err := r.Close()
		if err != nil {
		}
	}(r)
	var st models.SyncState
	if err := json.NewDecoder(r).Decode(&st); err != nil {
		return err
	}
	sm.stateMutex.Lock()
	sm.syncState = &st
	sm.stateMutex.Unlock()
	return nil
}

// saveSyncState 持久化同步状态（优先DB，否则S3）。
// 为什么：使同步进度可恢复与可审计，避免进程重启后丢失记录。
func (sm *SyncManager) saveSyncState(ctx context.Context) error {
	if sm.store != nil {
		return sm.store.SaveSyncState(ctx, sm.syncState)
	}
	key := sm.s3Client.GetSyncStateKey()
	b, err := json.Marshal(sm.syncState)
	if err != nil {
		return err
	}
	return sm.s3Client.UploadFile(ctx, key, bytes.NewReader(b), "application/json", int64(len(b)))
}
