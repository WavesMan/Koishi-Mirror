package sync

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"sort"
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

// SyncManager 同步管理器
type SyncManager struct {
	config     *config.Config
	s3Client   *s3client.Client
	syncState  *models.SyncState
	stateMutex sync.RWMutex
	store      *pgstore.Store
	ds         *datasource.Source
	logger     *logging.Logger
}

// NewSyncManager 创建新的同步管理器
func NewSyncManager(
	cfg *config.Config,
	s3Client *s3client.Client) *SyncManager {
	return &SyncManager{
		config:   cfg,
		s3Client: s3Client,
		syncState: &models.SyncState{
			PackageStates: make(map[string]models.PackageState),
		},
	}
}

func (sm *SyncManager) SetStore(store *pgstore.Store) {
	sm.store = store
}

// GetSyncState 获取当前同步状态
func (sm *SyncManager) GetSyncState() *models.SyncState {
	sm.stateMutex.RLock()
	defer sm.stateMutex.RUnlock()
	return sm.syncState
}

// Pull 拉取数据源并同步包到S3
func (sm *SyncManager) Pull(ctx context.Context) error {
	// 1. 拉取数据源（带缓存与重试）
	if sm.logger != nil {
		sm.logger.Info(
			"sync", "开始拉取数据源",
			map[string]interface{}{"url": sm.config.DataSourceURL})
	} else {
		fmt.Printf("开始拉取数据源: %s\n", sm.config.DataSourceURL)
	}
	var data models.DataSource
	var err error
	if sm.ds != nil {
		data, err = sm.ds.Get(ctx)
		if err != nil {
			return fmt.Errorf("拉取数据源失败: %v", err)
		}
	} else {
		return fmt.Errorf("数据源未配置")
	}
	if sm.logger != nil {
		sm.logger.Info(
			"sync", "发现数据源包",
			map[string]interface{}{"total": data.Total})
	} else {
		fmt.Printf("发现 %d 个包\n", data.Total)
	}

	// 3. 更新同步状态
	sm.stateMutex.Lock()
	sm.syncState.LastSyncTime = time.Now()
	sm.syncState.TotalPackages = data.Total
	sm.syncState.SyncedPackages = 0
	sm.syncState.FailedPackages = 0
	sm.stateMutex.Unlock()

	// 4. 加载上次同步状态
	if err := sm.loadSyncState(ctx); err != nil {
		fmt.Printf(
			"加载同步状态失败，将重新同步所有包: %v\n", err,
		)
	}

	// 5. 确定需要同步的包
	packagesToSync := sm.determinePackagesToSync(data.Packages)
	if sm.logger != nil {
		sm.logger.Info(
			"sync", "需要同步包",
			map[string]interface{}{"count": len(packagesToSync)})
	} else {
		fmt.Printf("需要同步 %d 个包\n", len(packagesToSync))
	}

	// 6. 并发同步包
	sem := make(chan struct{}, sm.config.Concurrency)
	var wg sync.WaitGroup
	var syncErrMutex sync.Mutex
	var syncErrors []error

	for _, pkg := range packagesToSync {
		wg.Add(1)
		sem <- struct{}{}

		go func(p models.Package) {
			defer wg.Done()
			defer func() { <-sem }()

			if err := sm.syncPackage(ctx, p); err != nil {
				syncErrMutex.Lock()
				syncErrors = append(syncErrors, fmt.Errorf(
					"同步包 %s@%s 失败: %v",
					p.Name, p.Version, err,
				))
				syncErrMutex.Unlock()
			}
		}(pkg)
	}

	wg.Wait()

	// 7. 保存同步状态
	if err := sm.saveSyncState(ctx); err != nil {
		if sm.logger != nil {
			sm.logger.Error(
				"sync", "保存同步状态失败",
				map[string]interface{}{"error": err.Error()})
		} else {
			fmt.Printf("保存同步状态失败: %v\n", err)
		}
	}

	// 8. 处理失败的包
	sm.retryFailedPackages(ctx)

	// 9. 返回错误（如果有）
	if len(syncErrors) > 0 {
		return fmt.Errorf("同步完成，但有 %d 个包同步失败: %v", len(syncErrors), syncErrors[0])
	}
	if sm.logger != nil {
		sm.logger.Info(
			"sync", "所有包同步完成", nil)
	} else {
		fmt.Println("所有包同步完成！")
	}
	return nil
}

func (sm *SyncManager) buildS3Index(ctx context.Context) (map[string]int64, error) {
	if sm.logger != nil {
		sm.logger.Debug("s3", "list_objects_start", nil)
	}
	idx := make(map[string]int64)
	objs, err := sm.s3Client.ListObjects(ctx, "", "")
	if err != nil {
		return idx, err
	}
	for _, o := range objs {
		k := *o.Key
		if !strings.HasSuffix(k, ".tgz") {
			continue
		}
		parts := strings.Split(k, "/")
		if len(parts) < 4 {
			continue
		}
		name := parts[len(parts)-3]
		version := parts[len(parts)-2]
		var sz int64
		if o.Size != nil {
			sz = *o.Size
		}
		idx[name+"@"+version] = sz
	}
	if sm.logger != nil {
		sm.logger.Debug("s3", "list_objects_done",
			map[string]interface{}{"count": len(objs), "tgz": len(idx)})
	}
	return idx, nil
}

func (sm *SyncManager) BootSync(ctx context.Context) error {
	sm.cleanCaches(ctx)
	_ = sm.loadSyncState(ctx)
	var recon *ReconcileSummary
	recon, _ = sm.Reconcile(ctx)

	// 2. 拉取数据源
	var data models.DataSource
	if sm.ds == nil {
		return fmt.Errorf("数据源未配置")
	}
	d, err := sm.ds.Get(ctx)
	if err != nil {
		return err
	}
	data = d

	s3idx, _ := sm.buildS3Index(ctx)

	// 4. 生成同步计划：S3不存在的版本需要同步
	var toSync []models.Package
	for _, pkg := range data.Packages {
		key := s3client.NormalizePackageName(pkg.Name) + "@" + pkg.Version
		if _, ok := s3idx[key]; !ok {
			toSync = append(toSync, pkg)
		}
	}

	if sm.isDebug() {
		sm.printInitSummary(ctx, data.Total, len(s3idx), recon, toSync)
	}

	if len(toSync) == 0 {
		return nil
	}

	// 5. 并发执行同步
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

func (sm *SyncManager) isDebug() bool {
	lv := strings.ToLower(sm.config.LogLevel)
	return lv == "debug" || lv == "trace"
}

func (sm *SyncManager) printInitSummary(
	ctx context.Context,
	dsTotal,
	s3Count int,
	recon *ReconcileSummary,
	toSync []models.Package) {

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

// determinePackagesToSync 确定需要同步的包
func (sm *SyncManager) determinePackagesToSync(packages []models.Package) []models.Package {
	sm.stateMutex.RLock()
	defer sm.stateMutex.RUnlock()

	var packagesToSync []models.Package

	for _, pkg := range packages {
		pkgKey := fmt.Sprintf("%s@%s", pkg.Name, pkg.Version)
		state, exists := sm.syncState.PackageStates[pkgKey]
		if !exists || state.SyncStatus == "failed" || (state.SyncStatus == "pending" && state.RetryCount < sm.config.MaxRetries) {
			packagesToSync = append(packagesToSync, pkg)
		}
	}

	if len(packagesToSync) > 1 {
		sort.SliceStable(packagesToSync, func(i, j int) bool {
			ai := strings.HasPrefix(packagesToSync[i].Name, "@koishijs/")
			aj := strings.HasPrefix(packagesToSync[j].Name, "@koishijs/")
			if ai != aj {
				return ai && !aj
			}
			return packagesToSync[i].Name < packagesToSync[j].Name
		})
	}

	return packagesToSync
}

// syncPackage 同步单个包
func (sm *SyncManager) syncPackage(ctx context.Context, pkg models.Package) error {
	pkgKey := fmt.Sprintf("%s@%s", pkg.Name, pkg.Version)

	// 更新包状态为同步中
	sm.updatePackageState(pkgKey, "syncing", 0)

	// 下载tar包到临时文件
	tempFile, err := os.CreateTemp("", "npm-*.tgz")
	if err != nil {
		sm.updatePackageState(pkgKey, "failed", 0)
		return fmt.Errorf("创建临时文件失败: %v", err)
	}
	defer os.Remove(tempFile.Name()) // 删除临时文件

	if sm.logger != nil {
		sm.logger.Debug(
			"sync", "download_start",
			map[string]interface{}{"name": pkg.Name, "version": pkg.Version, "url": pkg.Dist.Tarball})
	}
	resp, err := http.Get(pkg.Dist.Tarball)
	if err != nil {
		sm.updatePackageState(pkgKey, "failed", 0)
		if sm.logger != nil {
			sm.logger.Error(
				"sync", "download_error",
				map[string]interface{}{"name": pkg.Name, "version": pkg.Version, "error": err.Error()})
		}
		return fmt.Errorf("下载包失败: %v", err)
	}
	defer resp.Body.Close()

	// 写入临时文件
	fileSize, err := io.Copy(tempFile, resp.Body)
	if err != nil {
		sm.updatePackageState(pkgKey, "failed", 0)
		if sm.logger != nil {
			sm.logger.Error(
				"sync", "write_temp_error",
				map[string]interface{}{"name": pkg.Name, "version": pkg.Version, "error": err.Error()})
		}
		return fmt.Errorf("写入临时文件失败: %v", err)
	}
	if sm.logger != nil {
		sm.logger.Debug(
			"sync", "download_done",
			map[string]interface{}{"name": pkg.Name, "version": pkg.Version, "bytes": fileSize})
	}

	// 重置文件指针
	if _, err := tempFile.Seek(0, 0); err != nil {
		sm.updatePackageState(pkgKey, "failed", 0)
		return fmt.Errorf("重置文件指针失败: %v", err)
	}

	// 校验并修复可能的 chunked 编码污染：
	// 规则：
	// - 若 gzip 魔数 (0x1F,0x8B) 不在偏移 0，则视为前部污染，截取从魔数开始的内容
	// - 若文件尾为 "0\r\n\r\n"（chunk 结尾），则去掉尾部 5 字节
	// 最终确保上传对象为纯 gzip tarball
	if _, err := tempFile.Seek(0, 0); err != nil {
		sm.updatePackageState(pkgKey, "failed", 0)
		return fmt.Errorf("重置文件指针失败: %v", err)
	}
	br := bufio.NewReader(tempFile)
	head, _ := br.Peek(64)
	gzipIdx := -1
	for i := 0; i+1 < len(head); i++ {
		if head[i] == 0x1F && head[i+1] == 0x8B {
			gzipIdx = i
			break
		}
	}
	// 计算尾部是否有 chunk 终止符
	info, _ := tempFile.Stat()
	total := info.Size()
	suffix := int64(0)
	if total >= 5 {
		if _, err := tempFile.Seek(total-5, 0); err == nil {
			tail := make([]byte, 5)
			if _, err := io.ReadFull(tempFile, tail); err == nil {
				if tail[0] == '0' && tail[1] == '\r' && tail[2] == '\n' && tail[3] == '\r' && tail[4] == '\n' {
					suffix = 5
				}
			}
		}
	}
	// 清理逻辑
	if gzipIdx != 0 || suffix > 0 {
		start := int64(0)
		if gzipIdx > 0 {
			start = int64(gzipIdx)
		}
		length := total - suffix - start
		if length <= 0 {
			sm.updatePackageState(pkgKey, "failed", 0)
			return fmt.Errorf("清理后长度异常: total=%d start=%d suffix=%d", total, start, suffix)
		}
		cleaned, err := os.CreateTemp("", "npm-clean-*.tgz")
		if err != nil {
			sm.updatePackageState(pkgKey, "failed", 0)
			return fmt.Errorf("创建清理临时文件失败: %v", err)
		}
		defer os.Remove(cleaned.Name())
		if _, err := tempFile.Seek(start, 0); err != nil {
			sm.updatePackageState(pkgKey, "failed", 0)
			return fmt.Errorf("定位清理起点失败: %v", err)
		}
		n, err := io.CopyN(cleaned, tempFile, length)
		if err != nil && err != io.EOF {
			sm.updatePackageState(pkgKey, "failed", 0)
			return fmt.Errorf("复制清理数据失败: %v", err)
		}
		if _, err := cleaned.Seek(0, 0); err != nil {
			sm.updatePackageState(pkgKey, "failed", 0)
			return fmt.Errorf("重置清理文件指针失败: %v", err)
		}
		tempFile.Close()
		tempFile = cleaned
		fileSize = n
		if sm.logger != nil {
			sm.logger.Info(
				"sync", "clean_chunk_fix",
				map[string]interface{}{"name": pkg.Name, "version": pkg.Version, "start": start, "suffix": suffix, "bytes": n})
		}
	} else {
		// 正常 gzip，无需清理，确保从头开始
		if _, err := tempFile.Seek(0, 0); err != nil {
			sm.updatePackageState(pkgKey, "failed", 0)
			return fmt.Errorf("重置文件指针失败: %v", err)
		}
	}

	// 上传到S3
	s3Key := sm.s3Client.GetPackageKey(pkg.Name, pkg.Version)
	if sm.logger != nil {
		sm.logger.Debug("s3", "upload_start",
			map[string]interface{}{"key": s3Key, "size": fileSize})
	}
	if err := sm.s3Client.UploadFile(ctx, s3Key, tempFile, "application/gzip", fileSize); err != nil {
		sm.updatePackageState(pkgKey, "failed", 0)
		if sm.logger != nil {
			sm.logger.Error("s3", "upload_error",
				map[string]interface{}{"key": s3Key, "error": err.Error()})
		}
		return fmt.Errorf("上传S3失败: %v", err)
	}
	if sm.logger != nil {
		sm.logger.Debug("s3", "upload_done",
			map[string]interface{}{"key": s3Key, "size": fileSize})
	}

	// 校验 S3 对象大小，确保与本地一致
	if info, err := sm.s3Client.GetFileInfo(ctx, s3Key); err == nil {
		var actual int64
		if info.Size != nil {
			actual = *info.Size
		}
		if actual != fileSize {
			if sm.logger != nil {
				sm.logger.Warn("s3", "size_mismatch_after_upload",
					map[string]interface{}{"key": s3Key, "local": fileSize, "s3": actual})
			}
			// 标记为失败以便后续重试
			sm.updatePackageState(pkgKey, "failed", actual)
			return fmt.Errorf("S3对象大小不一致: 本地=%d S3=%d", fileSize, actual)
		}
	}
	_ = os.Remove(tempFile.Name())

	// 更新包元数据
	pkg.Dist.Size = fileSize
	if err := sm.savePackageMetadata(ctx, pkg); err != nil {
		if sm.logger != nil {
			sm.logger.Error(
				"sync", "save_meta_error",
				map[string]interface{}{"name": pkg.Name, "version": pkg.Version, "error": err.Error()})
		} else {
			fmt.Printf("保存包元数据失败 %s@%s: %v\n", pkg.Name, pkg.Version, err)
		}
	}

	// 更新包状态为同步成功
	sm.updatePackageState(pkgKey, "success", fileSize)
	if sm.logger != nil {
		sm.logger.Info(
			"sync", "同步成功",
			map[string]interface{}{"name": pkg.Name, "version": pkg.Version, "key": s3Key, "size": fileSize})
	} else {
		fmt.Printf("同步成功: %s@%s -> S3:%s (大小: %d bytes)\n", pkg.Name, pkg.Version, s3Key, fileSize)
	}

	if sm.store != nil {
		_ = sm.store.UpsertPackageVersion(ctx, pkg.Name, pkg.Description, pkg.Author, pkg)
		_ = sm.store.UpdateVersionStatus(ctx, pkg.Name, pkg.Version, "success", fileSize)
	}

	return nil
}

type ReconcileSummary struct {
	Matched         int      `json:"matched"`
	DBMissingS3     []string `json:"dbMissingS3"`
	S3MissingDB     []string `json:"s3MissingDb"`
	SizeMismatched  []string `json:"sizeMismatched"`
	UpdatedDB       []string `json:"updatedDb"`
	ScheduledResync []string `json:"scheduledResync"`
}

func (sm *SyncManager) Reconcile(ctx context.Context) (*ReconcileSummary, error) {
	if sm.store == nil {
		return &ReconcileSummary{}, nil
	}
	sum := &ReconcileSummary{}
	rows, err := sm.store.ListAllVersions(ctx)
	if err != nil {
		return nil, err
	}
	for _, r := range rows {
		name := r.Name
		version := r.Version
		size := r.Size
		key := sm.s3Client.GetPackageKey(name, version)
		info, err := sm.s3Client.GetFileInfo(ctx, key)
		if err != nil {
			if sm.logger != nil {
				sm.logger.Debug("s3", "head_missing",
					map[string]interface{}{"name": name, "version": version})
			}
			sum.DBMissingS3 = append(sum.DBMissingS3, name+"@"+version)
			_ = sm.store.UpdateVersionStatus(ctx, name, version, "failed", size)
			sum.ScheduledResync = append(sum.ScheduledResync, name+"@"+version)
			continue
		}
		var actual int64
		if info.Size != nil {
			actual = *info.Size
		}
		if actual != size {
			if sm.logger != nil {
				sm.logger.Debug("s3", "size_mismatch",
					map[string]interface{}{"name": name, "version": version, "db": size, "s3": actual})
			}
			_ = sm.store.UpdateVersionStatus(ctx, name, version, "success", actual)
			sum.SizeMismatched = append(sum.SizeMismatched, name+"@"+version)
			sum.UpdatedDB = append(sum.UpdatedDB, name+"@"+version)
		} else {
			sum.Matched++
		}
	}
    objs, err := sm.s3Client.ListObjects(ctx, "", "")
	if err == nil {
		for _, o := range objs {
			k := *o.Key
			if !strings.HasSuffix(k, ".tgz") {
				continue
			}
			parts := strings.Split(k, "/")
			if len(parts) < 4 {
				continue
			}
			name := parts[len(parts)-3]
			version := parts[len(parts)-2]
			ok, err := sm.store.HasVersion(ctx, name, version)
			if err != nil {
				continue
			}
			if !ok {
				if sm.logger != nil {
					sm.logger.Debug("s3", "db_insert_missing",
						map[string]interface{}{"name": name, "version": version})
				}
				sum.S3MissingDB = append(sum.S3MissingDB, name+"@"+version)
				pv := models.Package{Name: name, Version: version}
				pv.Dist.Tarball = "/download/" + version + "/" + url.PathEscape(name)
				if o.Size != nil {
					pv.Dist.Size = *o.Size
				}
				pv.SyncStatus = "success"
				pv.SyncTime = time.Now()
				_ = sm.store.UpsertPackageVersion(ctx, name, "", "", pv)
				var sz int64
				if o.Size != nil {
					sz = *o.Size
				}
				_ = sm.store.UpdateVersionStatus(ctx, name, version, "success", sz)
			}
		}
    }
    if sm.store != nil && sm.ds != nil {
        d, err := sm.ds.Get(ctx)
        if err == nil {
            for _, pkg := range d.Packages {
                key := sm.s3Client.GetPackageKey(pkg.Name, pkg.Version)
                info, err := sm.s3Client.GetFileInfo(ctx, key)
                if err == nil {
                    var sz int64
                    if info.Size != nil {
                        sz = *info.Size
                    }
                    pkg.Dist.Tarball = "/download/" + pkg.Version + "/" + url.PathEscape(pkg.Name)
                    pkg.Dist.Size = sz
                    pkg.SyncStatus = "success"
                    pkg.SyncTime = time.Now()
                    _ = sm.store.UpsertPackageVersion(ctx, pkg.Name, pkg.Description, pkg.Author, pkg)
                    _ = sm.store.UpdateVersionStatus(ctx, pkg.Name, pkg.Version, "success", sz)
                } else {
                    ok, _ := sm.store.HasVersion(ctx, pkg.Name, pkg.Version)
                    if !ok {
                        pv := models.Package{Name: pkg.Name, Version: pkg.Version}
                        pv.SyncStatus = "pending"
                        pv.SyncTime = time.Now()
                        _ = sm.store.UpsertPackageVersion(ctx, pkg.Name, pkg.Description, pkg.Author, pv)
                    } else {
                        _ = sm.store.UpdateVersionStatus(ctx, pkg.Name, pkg.Version, "failed", 0)
                    }
                }
            }
        }
    }
    return sum, nil
}

// updatePackageState 更新包状态
func (sm *SyncManager) updatePackageState(pkgKey, status string, size int64) {
	sm.stateMutex.Lock()
	defer sm.stateMutex.Unlock()

	state, exists := sm.syncState.PackageStates[pkgKey]
	if !exists {
		state = models.PackageState{}
	}

	// 更新状态
	state.SyncStatus = status
	state.SyncTime = time.Now()

	// 如果是失败状态，增加重试次数
	if status == "failed" {
		state.RetryCount++
	}

	// 如果是成功状态，设置大小
	if status == "success" {
		state.Size = size
	}

	sm.syncState.PackageStates[pkgKey] = state

	// 更新统计信息
	sm.updateSyncStats()
}

// updateSyncStats 更新同步统计信息
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

// retryFailedPackages 重试失败的包
func (sm *SyncManager) retryFailedPackages(ctx context.Context) {
	sm.stateMutex.RLock()
	var failedPackages []models.Package
	for pkgKey, state := range sm.syncState.PackageStates {
		if state.SyncStatus == "failed" && state.RetryCount < sm.config.MaxRetries {
			// 解析包名和版本
			var name, version string
			fmt.Sscanf(pkgKey, "%s@%s", &name, &version)

			// 创建包对象
			pkg := models.Package{
				Name:    name,
				Version: version,
			}

			failedPackages = append(failedPackages, pkg)
		}
	}
	sm.stateMutex.RUnlock()

	if len(failedPackages) == 0 {
		return
	}

	fmt.Printf("开始重试 %d 个失败的包，最大重试次数: %d\n", len(failedPackages), sm.config.MaxRetries)

	// 并发重试
	sem := make(chan struct{}, sm.config.Concurrency)
	var wg sync.WaitGroup

	for _, pkg := range failedPackages {
		wg.Add(1)
		sem <- struct{}{}

		go func(p models.Package) {
			defer wg.Done()
			defer func() { <-sem }()

			fmt.Printf("重试同步: %s@%s\n", p.Name, p.Version)
			// 重新获取包的完整信息
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

	// 保存更新后的状态
	if err := sm.saveSyncState(ctx); err != nil {
		fmt.Printf("保存同步状态失败: %v\n", err)
	}
}

// fetchPackageInfo 获取包的完整信息
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

// savePackageMetadata 保存包元数据
func (sm *SyncManager) savePackageMetadata(ctx context.Context, pkg models.Package) error {
	// 将元数据序列化为JSON
	metadataJSON, err := json.Marshal(pkg)
	if err != nil {
		return fmt.Errorf("序列化元数据失败: %v", err)
	}

	// 上传到S3
	metaKey := sm.s3Client.GetPackageMetaKey(pkg.Name)
	return sm.s3Client.UploadFile(ctx, metaKey, bytes.NewReader(metadataJSON), "application/json", int64(len(metadataJSON)))
}

// loadSyncState 从S3加载同步状态
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
	defer r.Close()
	var st models.SyncState
	if err := json.NewDecoder(r).Decode(&st); err != nil {
		return err
	}
	sm.stateMutex.Lock()
	sm.syncState = &st
	sm.stateMutex.Unlock()
	return nil
}

// saveSyncState 保存同步状态到S3
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

func (sm *SyncManager) cleanCaches(ctx context.Context) {
	if sm.ds != nil {
		sm.ds.ClearCache(ctx)
	}
	tmp := os.TempDir()
	if entries, err := os.ReadDir(tmp); err == nil {
		for _, e := range entries {
			n := e.Name()
			if strings.HasPrefix(n, "npm-") && strings.HasSuffix(n, ".tgz") {
				_ = os.Remove(tmp + string(os.PathSeparator) + n)
			}
		}
	}
	if sm.store != nil {
		_ = sm.store.PurgeLogsOlderThan(ctx, 30)
	}
}

// StartCron 启动定时同步任务
func (sm *SyncManager) StartCron(ctx context.Context) {
	// 首次立即执行
	sm.cleanCaches(ctx)
	if err := sm.Pull(ctx); err != nil {
		fmt.Printf("首次同步失败: %v\n", err)
	}

	// 设置定时任务
	ticker := time.NewTicker(sm.config.SyncInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			fmt.Printf("\n定时同步触发（%s）\n", time.Now().Format("2006-01-02 15:04:05"))
			sm.cleanCaches(ctx)
			if err := sm.Pull(ctx); err != nil {
				fmt.Printf("定时同步失败: %v\n", err)
			}
		case <-ctx.Done():
			fmt.Println("定时同步任务已停止")
			return
		}
	}
}
func (sm *SyncManager) SetDataSource(ds *datasource.Source) {
	sm.ds = ds
}
func (sm *SyncManager) SetLogger(l *logging.Logger)        { sm.logger = l }
func (sm *SyncManager) RetryFailedNow(ctx context.Context) { sm.retryFailedPackages(ctx) }
