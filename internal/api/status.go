package api

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"npm-mirror/internal/models"
)

// GetMirrorStatus NOTE: 状态统计接口集中此处，统一数据来源（DB 优先、内存回退），保证接口稳定性。
func (h *Handler) GetMirrorStatus(c *gin.Context) {
	syncState := h.syncManager.GetSyncState()
	total := syncState.TotalPackages
	if h.ds != nil {
		if d, err := h.ds.Get(c.Request.Context()); err == nil {
			if d.Total > 0 {
				total = d.Total
			}
		}
	}

	var storageSize int64
	synced := 0
	failed := 0
	lastSync := syncState.LastSyncTime
	if h.store != nil {
		if m, err := h.store.StatusCounts(c.Request.Context()); err == nil {
			if v, ok := m["success"]; ok {
				synced = v
			}
			if v, ok := m["failed"]; ok {
				failed = v
			}
		}
		if sz, err := h.store.TotalSuccessSize(c.Request.Context()); err == nil {
			storageSize = sz
		}
		if t, err := h.store.LatestSyncTime(c.Request.Context()); err == nil {
			lastSync = t
		}
	} else {
		for _, state := range syncState.PackageStates {
			if state.SyncStatus == "success" {
				synced++
				storageSize += state.Size
			} else if state.SyncStatus == "failed" {
				failed++
			}
		}
	}

	status := models.MirrorStatus{LastSyncTime: lastSync, TotalPackages: total, SyncedPackages: synced, FailedPackages: failed, StorageSize: storageSize, S3Bucket: h.config.TencentCosbucket, DataSourceURL: h.config.DataSourceURL}
	breakdown := map[string]int{"success": synced, "failed": failed}
	if h.store != nil {
		if m, err := h.store.StatusCounts(c.Request.Context()); err == nil {
			breakdown = m
		}
	} else {
		pending := 0
		syncing := 0
		for _, state := range syncState.PackageStates {
			switch state.SyncStatus {
			case "pending":
				pending++
			case "syncing":
				syncing++
			}
		}
		breakdown["pending"] = pending
		breakdown["syncing"] = syncing
	}
	status.StatusBreakdown = breakdown
	c.JSON(http.StatusOK, status)
}

func (h *Handler) GetMirrorCOSStatus(c *gin.Context) {
	syncState := h.syncManager.GetSyncState()
	total := syncState.TotalPackages
	if h.ds != nil {
		if d, err := h.ds.Get(c.Request.Context()); err == nil {
			if d.Total > 0 {
				total = d.Total
			}
		}
	}

	var storageSize int64
	synced := 0
	failed := 0
	lastSync := syncState.LastSyncTime
	if h.store != nil {
		if m, err := h.store.StatusCounts(c.Request.Context()); err == nil {
			if v, ok := m["success"]; ok {
				synced = v
			}
			if v, ok := m["failed"]; ok {
				failed = v
			}
		}
		if sz, err := h.store.TotalSuccessSize(c.Request.Context()); err == nil {
			storageSize = sz
		}
		if t, err := h.store.LatestSyncTime(c.Request.Context()); err == nil {
			lastSync = t
		}
	} else {
		for _, state := range syncState.PackageStates {
			if state.SyncStatus == "success" {
				synced++
				storageSize += state.Size
			} else if state.SyncStatus == "failed" {
				failed++
			}
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"totalPackages":  total,
		"syncedPackages": synced,
		"failedPackages": failed,
		"storageSize":    storageSize,
		"lastSyncTime":   lastSync,
		"dataSourceURL":  h.config.DataSourceURL,
		"icpEnabled":     h.config.ICPEnabled,
		"icpRecord":      h.config.ICPRecord,
		"icpUrl":         h.config.ICPUrl,
		"securityRecord": h.config.SecurityRecord,
		"securityUrl":    h.config.SecurityUrl,
	})
}

func (h *Handler) GetStorageStats(c *gin.Context) {
	if h.store != nil {
		versions, err := h.store.ListAllVersions(c.Request.Context())
		if err == nil && len(versions) > 0 {
			stats := models.StorageStats{TotalSize: 0, PackageCount: 0, VersionCount: len(versions), PackageStats: make(map[string]models.PackageStat)}
			packageVersions := make(map[string][]int64)
			for _, v := range versions {
				stats.TotalSize += v.Size
				packageVersions[v.Name] = append(packageVersions[v.Name], v.Size)
			}
			stats.PackageCount = len(packageVersions)
			for name, sizes := range packageVersions {
				if len(sizes) == 0 {
					continue
				}
				stat := models.PackageStat{VersionCount: len(sizes), TotalSize: 0, MaxSize: sizes[0], MinSize: sizes[0]}
				for _, size := range sizes {
					stat.TotalSize += size
					if size > stat.MaxSize {
						stat.MaxSize = size
					}
					if size < stat.MinSize {
						stat.MinSize = size
					}
				}
				stat.AvgSize = float64(stat.TotalSize) / float64(stat.VersionCount)
				stats.PackageStats[name] = stat
			}
			c.JSON(http.StatusOK, stats)
			return
		}
	}

	syncState := h.syncManager.GetSyncState()
	stats := models.StorageStats{TotalSize: 0, PackageCount: 0, VersionCount: len(syncState.PackageStates), PackageStats: make(map[string]models.PackageStat)}
	packageVersions := make(map[string][]int64)
	for pkgKey, state := range syncState.PackageStates {
		if state.SyncStatus == "success" {
			name := strings.Split(pkgKey, "@")[0]
			stats.TotalSize += state.Size
			packageVersions[name] = append(packageVersions[name], state.Size)
		}
	}
	stats.PackageCount = len(packageVersions)
	for name, sizes := range packageVersions {
		if len(sizes) == 0 {
			continue
		}
		stat := models.PackageStat{VersionCount: len(sizes), TotalSize: 0, MaxSize: sizes[0], MinSize: sizes[0]}
		for _, size := range sizes {
			stat.TotalSize += size
			if size > stat.MaxSize {
				stat.MaxSize = size
			}
			if size < stat.MinSize {
				stat.MinSize = size
			}
		}
		stat.AvgSize = float64(stat.TotalSize) / float64(stat.VersionCount)
		stats.PackageStats[name] = stat
	}
	c.JSON(http.StatusOK, stats)
}
