package api

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"npm-mirror/config"
	"npm-mirror/internal/datasource"
	"npm-mirror/internal/models"
	"npm-mirror/internal/s3client"
	pgstore "npm-mirror/internal/storage/postgres"
	"npm-mirror/internal/sync"
	"npm-mirror/internal/version"
)

// Handler API处理器
type Handler struct {
	config      *config.Config
	s3Client    *s3client.Client
	syncManager *sync.SyncManager
	vc          *version.Controller
	ds          *datasource.Source
	store       *pgstore.Store
}

// NewHandler 创建新的API处理器
func NewHandler(cfg *config.Config, s3Client *s3client.Client, syncManager *sync.SyncManager, store *pgstore.Store, ds *datasource.Source) *Handler {
	return &Handler{
		config:      cfg,
		s3Client:    s3Client,
		syncManager: syncManager,
		vc:          version.NewController(cfg, s3Client, syncManager, store),
		ds:          ds,
		store:       store,
	}
}

// RegisterRoutes 注册路由
func (h *Handler) RegisterRoutes(r *gin.Engine) {
	r.Use(func(c *gin.Context) {
		p := c.Request.URL.Path
		if c.Request.Method == "GET" {
			if strings.HasPrefix(p, "/assets/") {
				c.Writer.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
			} else if p == "/" || strings.HasSuffix(p, "index.html") {
				c.Writer.Header().Set("Cache-Control", "no-cache")
			}
		}
		c.Next()
	})
	// 前端接口
    r.GET("/api/status", h.GetMirrorCOSStatus)
    r.GET("/api/mirror-status", h.GetMirrorStatus)
	r.GET("/api/packages", h.GetPackages)
	// 详情使用独立前缀，避免与 /api/packages/:name/versions 路由冲突
	r.GET("/api/package/*name", h.GetPackageDetail)
	r.GET("/api/packages/:name/versions", h.GetPackageVersions)
	r.GET("/api/packages/:name/dist-tags", h.GetPackageDistTags)
	r.GET("/api/resolve", h.ResolveVersion)
	r.GET("/api/stats", h.GetStorageStats)
	r.POST("/api/sync", h.TriggerSync)
    r.POST("/api/reconcile", h.Reconcile)
    r.POST("/api/boot-sync", h.BootSync)
	r.POST("/api/retry-failed", h.RetryFailed)
	r.POST("/api/log-test", h.LogTest)
	r.GET("/api/index", h.GetIndex)
	r.GET("/api/registry", h.GetRegistry)
	r.GET("/-/v1/search", h.SearchV1)
	r.POST("/-/npm/v1/security/advisories/bulk", h.AdvisoriesBulk)

	// 静态资源
	r.Static("/assets", "./ui/dist/assets")
	r.StaticFile("/favicon.ico", "./ui/dist/favicon.ico")
	r.StaticFile("/koishi.png", "./ui/dist/koishi.png")

	// npm客户端兼容接口：使用 NoRoute 作为回退以避免与 /api 路由冲突
	r.NoRoute(h.RegistryFallback)
	r.GET("/download/:version/*name", h.DownloadPackage)
	r.GET("/index.json", h.GetIndex)
}

func (h *Handler) LogTest(c *gin.Context) {
	if h.store == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "store not configured"})
		return
	}
	fields := map[string]interface{}{"ok": true, "time": time.Now().Format(time.RFC3339)}
	if err := h.store.InsertLog(c.Request.Context(), "info", "health", "db_log_test", fields); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "ok"})
}

func (h *Handler) Reconcile(c *gin.Context) {
	sum, err := h.syncManager.Reconcile(c.Request.Context())
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, sum)
}

func (h *Handler) GetPackageVersions(c *gin.Context) {
	name := c.Param("name")
	md, err := h.vc.BuildPackageMetadata(c.Request.Context(), name)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	
	// 补充同步状态信息
	syncState := h.syncManager.GetSyncState()
	versionsWithStatus := make([]gin.H, 0, len(md.Versions))
	for _, v := range md.Versions {
		pkgKey := fmt.Sprintf("%s@%s", v.Name, v.Version)
		syncStatus := "pending"
		var syncTime time.Time
		if state, exists := syncState.PackageStates[pkgKey]; exists {
			syncStatus = state.SyncStatus
			syncTime = state.SyncTime
		}
		versionsWithStatus = append(versionsWithStatus, gin.H{
			"name":       v.Name,
			"version":    v.Version,
			"dist":       v.Dist,
			"syncStatus": syncStatus,
			"syncTime":   syncTime,
		})
	}
	
	c.JSON(http.StatusOK, gin.H{"name": name, "versions": versionsWithStatus})
}

func (h *Handler) GetPackageDistTags(c *gin.Context) {
	name := c.Param("name")
	md, err := h.vc.BuildPackageMetadata(c.Request.Context(), name)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	
	// 补充同步状态信息
	syncState := h.syncManager.GetSyncState()
	distTagsWithStatus := make(map[string]gin.H)
	for tag, version := range md.DistTags {
		pkgKey := fmt.Sprintf("%s@%s", name, version)
		syncStatus := "pending"
		var syncTime time.Time
		if state, exists := syncState.PackageStates[pkgKey]; exists {
			syncStatus = state.SyncStatus
			syncTime = state.SyncTime
		}
		distTagsWithStatus[tag] = gin.H{
			"version":    version,
			"syncStatus": syncStatus,
			"syncTime":   syncTime,
		}
	}
	
	c.JSON(http.StatusOK, gin.H{"name": name, "distTags": distTagsWithStatus})
}

func (h *Handler) ResolveVersion(c *gin.Context) {
	name := c.Query("name")
	rng := c.Query("range")
	v, err := h.vc.ResolveVersion(c.Request.Context(), name, rng)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	
	// 补充同步状态信息
	syncState := h.syncManager.GetSyncState()
	pkgKey := fmt.Sprintf("%s@%s", name, v)
	syncStatus := "pending"
	var syncTime time.Time
	if state, exists := syncState.PackageStates[pkgKey]; exists {
		syncStatus = state.SyncStatus
		syncTime = state.SyncTime
	}
	
	c.JSON(http.StatusOK, gin.H{
		"name":       name,
		"version":    v,
		"syncStatus": syncStatus,
		"syncTime":   syncTime,
	})
}

// GetMirrorCOSStatus 获取镜像状态
// GetMirrorStatus 获取完整镜像状态（含配置项）
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

    status := models.MirrorStatus{
        LastSyncTime:   lastSync,
        TotalPackages:  total,
        SyncedPackages: synced,
        FailedPackages: failed,
        StorageSize:    storageSize,
        S3Bucket:       h.config.TENCENT_COSBucket,
        DataSourceURL:  h.config.DataSourceURL,
    }
    // 追加状态分布
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

    status.ICPEnabled = h.config.ICPEnabled
    status.ICPRecord = h.config.ICPRecord
    status.ICPUrl = h.config.ICPUrl
    status.SecurityRecord = h.config.SecurityRecord
    status.SecurityUrl = h.config.SecurityUrl

    c.JSON(http.StatusOK, status)
}

// GetMirrorCOSStatus 获取基本镜像状态（不含配置项）
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
    })
}

func (h *Handler) BootSync(c *gin.Context) {
    go func() {
        ctx := context.Background()
        _ = h.syncManager.BootSync(ctx)
    }()
    c.JSON(http.StatusOK, gin.H{"message": "已触发初始化同步"})
}

// GetPackages 获取包列表
func (h *Handler) GetPackages(c *gin.Context) {
	// 获取查询参数
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("pageSize", "50"))
	search := c.Query("search")

	// 验证参数
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 100 {
		pageSize = 50
	}

	// 拉取数据源
	data, err := h.ds.Get(c.Request.Context())
	if err != nil {
		c.JSON(500, gin.H{"error": "拉取数据源失败"})
		return
	}

	// 搜索过滤
	packages := data.Packages
	if search != "" {
		var filtered []models.Package
		for _, pkg := range packages {
			if strings.Contains(pkg.Name, search) {
				filtered = append(filtered, pkg)
			}
		}
		packages = filtered
	}

	// 分页
	start := (page - 1) * pageSize
	end := start + pageSize
	if start >= len(packages) {
		start = 0
		end = 0
	}
	if end > len(packages) {
		end = len(packages)
	}

	var pagedPackages []models.Package
	if end > start {
		pagedPackages = packages[start:end]
	}

	// 获取同步状态
	syncState := h.syncManager.GetSyncState()

	// 补充同步状态信息
	for i := range pagedPackages {
		pkgKey := fmt.Sprintf("%s@%s", pagedPackages[i].Name, pagedPackages[i].Version)
		if state, exists := syncState.PackageStates[pkgKey]; exists {
			pagedPackages[i].SyncStatus = state.SyncStatus
			pagedPackages[i].SyncTime = state.SyncTime
			pagedPackages[i].RetryCount = state.RetryCount
			if state.Size > 0 {
				pagedPackages[i].Dist.Size = state.Size
			}
		} else {
			pagedPackages[i].SyncStatus = "pending"
		}
	}

	response := models.PackageListResponse{
		Total:    len(packages),
		Packages: pagedPackages,
		Page:     page,
		PageSize: pageSize,
	}

	c.JSON(http.StatusOK, response)
}

// GetPackageDetail 获取包详情
func (h *Handler) GetPackageDetail(c *gin.Context) {
	name := c.Param("name")
	// 当路由使用 *name 捕获时，值可能以 / 开头，并可能包含编码
	if strings.HasPrefix(name, "/") {
		name = name[1:]
	}
	if u, err := url.PathUnescape(name); err == nil {
		name = u
	}
	name = strings.ReplaceAll(name, "%2F", "/")
	name = strings.ReplaceAll(name, "%2f", "/")

	// 拉取数据源
	data, err := h.ds.Get(c.Request.Context())
	if err != nil {
		c.JSON(500, gin.H{"error": "拉取数据源失败"})
		return
	}

	// 查找包
	var versions []struct {
		Version string `json:"version"`
		Dist    struct {
			Tarball string `json:"tarball"`
			Size    int64  `json:"size,omitempty"`
			Shasum  string `json:"shasum,omitempty"`
		} `json:"dist"`
		SyncStatus string    `json:"syncStatus"`
		SyncTime   time.Time `json:"syncTime,omitempty"`
	}

	var detail models.PackageDetail
	found := false

	for _, pkg := range data.Packages {
		if pkg.Name == name {
			if !found {
				detail.Name = pkg.Name
				detail.Description = pkg.Description
				detail.Author = pkg.Author
				found = true
			}

			// 获取同步状态
			pkgKey := fmt.Sprintf("%s@%s", pkg.Name, pkg.Version)
			syncState := h.syncManager.GetSyncState()
			syncStatus := "pending"
			syncTime := time.Time{}

			if state, exists := syncState.PackageStates[pkgKey]; exists {
				syncStatus = state.SyncStatus
				syncTime = state.SyncTime
				if state.Size > 0 {
					pkg.Dist.Size = state.Size
				}
			}

			// 添加版本信息
			version := struct {
				Version string `json:"version"`
				Dist    struct {
					Tarball string `json:"tarball"`
					Size    int64  `json:"size,omitempty"`
					Shasum  string `json:"shasum,omitempty"`
				} `json:"dist"`
				SyncStatus string    `json:"syncStatus"`
				SyncTime   time.Time `json:"syncTime,omitempty"`
			}{
				Version:    pkg.Version,
				SyncStatus: syncStatus,
				SyncTime:   syncTime,
			}
			version.Dist.Tarball = fmt.Sprintf("/download/%s/%s", pkg.Version, url.PathEscape(pkg.Name))
			version.Dist.Size = pkg.Dist.Size
			version.Dist.Shasum = pkg.Dist.Shasum

			versions = append(versions, version)
		}
	}

	if !found {
		c.JSON(http.StatusNotFound, gin.H{"error": "包不存在"})
		return
	}

	detail.Versions = versions
	c.JSON(http.StatusOK, detail)
}

// GetStorageStats 获取存储统计
func (h *Handler) GetStorageStats(c *gin.Context) {
	syncState := h.syncManager.GetSyncState()

	// 计算存储统计
	stats := models.StorageStats{
		TotalSize:    0,
		PackageCount: 0,
		VersionCount: len(syncState.PackageStates),
		PackageStats: make(map[string]models.PackageStat),
	}

	// 按包名分组统计
	packageVersions := make(map[string][]int64)

	for pkgKey, state := range syncState.PackageStates {
		if state.SyncStatus == "success" {
			// 解析包名
			name := strings.Split(pkgKey, "@")[0]

			// 累加总大小
			stats.TotalSize += state.Size

			// 按包名分组
			packageVersions[name] = append(packageVersions[name], state.Size)
		}
	}

	// 计算每个包的统计信息
	stats.PackageCount = len(packageVersions)
	for name, sizes := range packageVersions {
		stat := models.PackageStat{
			VersionCount: len(sizes),
			TotalSize:    0,
			MaxSize:      sizes[0],
			MinSize:      sizes[0],
		}

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

// TriggerSync 触发同步
func (h *Handler) TriggerSync(c *gin.Context) {
	go func() {
		ctx := context.Background()
		if err := h.syncManager.Pull(ctx); err != nil {
			fmt.Printf("手动触发同步失败: %v\n", err)
		}
	}()

	c.JSON(http.StatusOK, gin.H{
		"message": "同步任务已触发",
		"time":    time.Now().Format(time.RFC3339),
	})
}

// GetPackageMeta 获取包元数据（npm客户端兼容）
func (h *Handler) GetPackageMeta(c *gin.Context) {
	raw := c.Param("name")
	scope := c.Param("scope")
	if scope != "" {
		if strings.HasPrefix(scope, "@") {
			raw = scope + "/" + raw
		} else {
			raw = "@" + scope + "/" + raw
		}
	}
	if strings.HasPrefix(raw, "/") {
		raw = raw[1:]
	}
	if u, err := url.PathUnescape(raw); err == nil {
		raw = u
	}
	raw = strings.ReplaceAll(raw, "%2F", "/")
	raw = strings.ReplaceAll(raw, "%2f", "/")
	if raw == "" {
		c.JSON(http.StatusNotFound, gin.H{"error": "包不存在"})
		return
	}

	md, err := h.vc.BuildPackageMetadata(c.Request.Context(), raw)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "包不存在"})
		return
	}

	syncState := h.syncManager.GetSyncState()
	host := c.Request.Host

	versions := map[string]gin.H{}
	for _, v := range md.Versions {
		key := v.Name + "@" + v.Version
		tar := v.Dist.Tarball
		if st, ok := syncState.PackageStates[key]; ok && st.SyncStatus == "success" {
			tar = fmt.Sprintf("http://%s/download/%s/%s", host, v.Version, url.PathEscape(v.Name))
		}
		versions[v.Version] = gin.H{
			"name":    v.Name,
			"version": v.Version,
			"dist": gin.H{
				"tarball": tar,
				"shasum":  v.Dist.Shasum,
				"size":    v.Dist.Size,
			},
		}
	}

	resp := gin.H{
		"name":      md.Name,
		"dist-tags": md.DistTags,
		"versions":  versions,
	}
	c.JSON(http.StatusOK, resp)
}

// DownloadPackage 下载包
func (h *Handler) DownloadPackage(c *gin.Context) {
	name := c.Param("name")
	if strings.HasPrefix(name, "/") {
		name = name[1:]
	}
	if u, err := url.PathUnescape(name); err == nil {
		name = u
	}
	name = strings.ReplaceAll(name, "%2F", "/")
	name = strings.ReplaceAll(name, "%2f", "/")
	version := c.Param("version")

	// 检查包是否存在且已同步
	syncState := h.syncManager.GetSyncState()
	pkgKey := fmt.Sprintf("%s@%s", name, version)

	state, exists := syncState.PackageStates[pkgKey]
	if !exists || state.SyncStatus != "success" {
		c.JSON(http.StatusNotFound, gin.H{"error": "包不存在或未同步完成"})
		return
	}

	// CDN URL添加端点头为存储桶名
	s3Key := h.s3Client.GetPackageKey(name, version)
	if h.config.CDNEnabled && h.config.CDNEndpoint != "" {
		// 修改处：手动拼接 URL，避免 BuildCDNURL 重复添加前缀
		// 确保格式为: CDN端点/存储桶名/文件路径
		endpoint := strings.TrimRight(h.config.CDNEndpoint, "/")
		u := fmt.Sprintf("%s/%s/%s", endpoint, s3Key)
		c.Redirect(http.StatusFound, u)
		return
	}
	downloadURL, err := h.s3Client.GetPresignedURL(c.Request.Context(), s3Key, 15*time.Minute)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "获取下载链接失败"})
		return
	}
	c.Redirect(http.StatusFound, downloadURL)
}

func (h *Handler) GetIndex(c *gin.Context) {
	data, err := h.ds.Get(c.Request.Context())
	if err != nil {
		c.JSON(500, gin.H{"error": "拉取数据源失败"})
		return
	}
	syncState := h.syncManager.GetSyncState()
	host := c.Request.Host
	for i := range data.Packages {
		key := fmt.Sprintf("%s@%s", data.Packages[i].Name, data.Packages[i].Version)
		if st, ok := syncState.PackageStates[key]; ok && st.SyncStatus == "success" {
			data.Packages[i].Dist.Tarball = fmt.Sprintf(
				"http://%s/download/%s/%s",
				host,
				data.Packages[i].Version,
				url.PathEscape(data.Packages[i].Name),
			)
			if st.Size > 0 {
				data.Packages[i].Dist.Size = st.Size
			}
		}
	}
	c.JSON(http.StatusOK, data)
}

func (h *Handler) GetRegistry(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"registry": fmt.Sprintf("http://%s/", c.Request.Host)})
}

// RegistryFallback 处理 npm 客户端的兼容请求（包元数据、搜索、审计）
func (h *Handler) RegistryFallback(c *gin.Context) {
	p := c.Request.URL.Path

	// SPA 回退：如果是浏览器请求且不是 API 请求，返回 index.html
	if strings.Contains(c.Request.Header.Get("Accept"), "text/html") {
		c.File("./ui/dist/index.html")
		return
	}

	// 兼容安全审计接口，返回空结果
	if strings.HasPrefix(p, "/-/npm/v1/security/advisories/bulk") {
		c.JSON(http.StatusOK, gin.H{"advisories": []interface{}{}, "objects": []interface{}{}})
		return
	}
	// 兼容搜索接口：从数据源返回匹配结果
	if strings.HasPrefix(p, "/-/v1/search") {
		q := c.Request.URL.Query()
		text := q.Get("text")
		size := 20
		from := 0
		if v := q.Get("size"); v != "" {
			if n, err := strconv.Atoi(v); err == nil {
				size = n
			}
		}
		if v := q.Get("from"); v != "" {
			if n, err := strconv.Atoi(v); err == nil {
				from = n
			}
		}
		var objs []gin.H
		if h.ds != nil {
			if data, err := h.ds.Get(c.Request.Context()); err == nil {
				for _, pkg := range data.Packages {
					if text == "" || strings.Contains(pkg.Name, text) {
						objs = append(objs, gin.H{
							"package": gin.H{
								"name":        pkg.Name,
								"version":     pkg.Version,
								"description": pkg.Description,
								"links": gin.H{
									"npm": fmt.Sprintf("http://%s/%s",
										c.Request.Host,
										pkg.Name,
									),
								},
							},
						})
					}
				}
			}
		}
		total := len(objs)
		// 简单分页
		end := from + size
		if end > total {
			end = total
		}
		if from < 0 {
			from = 0
		}
		if from > end {
			from = end
		}
		c.JSON(http.StatusOK, gin.H{
			"objects": objs[from:end],
			"total":   total,
			"time":    time.Now().Format(time.RFC3339),
		})
		return
	}
	// 包元数据：如 /name 或 /@scope/name
	raw := strings.TrimPrefix(p, "/")
	if raw == "" || strings.HasPrefix(raw, "api/") || strings.HasPrefix(raw, "download/") {
		c.JSON(http.StatusNotFound, gin.H{"error": "未匹配的路由"})
		return
	}
	if u, err := url.PathUnescape(raw); err == nil {
		raw = u
	}
	raw = strings.ReplaceAll(raw, "%2F", "/")
	raw = strings.ReplaceAll(raw, "%2f", "/")

	md, err := h.vc.BuildPackageMetadata(c.Request.Context(), raw)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "包不存在"})
		return
	}

	syncState := h.syncManager.GetSyncState()
	host := c.Request.Host

	versions := map[string]gin.H{}
	for _, v := range md.Versions {
		key := v.Name + "@" + v.Version
		tar := v.Dist.Tarball
		if st, ok := syncState.PackageStates[key]; ok && st.SyncStatus == "success" {
			tar = fmt.Sprintf("http://%s/download/%s/%s", host, v.Version, v.Name)
		}
		versions[v.Version] = gin.H{
			"name":    v.Name,
			"version": v.Version,
			"dist": gin.H{
				"tarball": tar,
				"shasum":  v.Dist.Shasum,
				"size":    v.Dist.Size,
			},
		}
	}

	resp := gin.H{
		"name":      md.Name,
		"dist-tags": md.DistTags,
		"versions":  versions,
	}
	c.JSON(http.StatusOK, resp)
}
func (h *Handler) RetryFailed(c *gin.Context) {
	go h.syncManager.RetryFailedNow(context.Background())
	c.JSON(http.StatusOK, gin.H{"message": "已触发失败重试"})
}
func (h *Handler) AdvisoriesBulk(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"advisories": []interface{}{}, "objects": []interface{}{}})
}

func (h *Handler) SearchV1(c *gin.Context) {
	q := c.Request.URL.Query()
	text := q.Get("text")
	size := 20
	from := 0
	if v := q.Get("size"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			size = n
		}
	}
	if v := q.Get("from"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			from = n
		}
	}

	var objs []gin.H
	if h.ds != nil {
		if data, err := h.ds.Get(c.Request.Context()); err == nil {
			for _, pkg := range data.Packages {
				if text == "" || strings.Contains(pkg.Name, text) {
					objs = append(objs, gin.H{
						"package": gin.H{
							"name":        pkg.Name,
							"scope":       scopeOf(pkg.Name),
							"version":     pkg.Version,
							"description": pkg.Description,
							"date":        time.Now().Format(time.RFC3339),
							"links": gin.H{
								"npm": fmt.Sprintf("http://%s/%s",
									c.Request.Host, pkg.Name,
								),
							},
							"publisher":   gin.H{"username": "", "email": ""},
							"maintainers": []interface{}{},
						},
						"score": gin.H{
							"final": 0.0,
							"detail": gin.H{
								"quality":     0.0,
								"popularity":  0.0,
								"maintenance": 0.0,
							},
						},
						"searchScore": 1.0,
					})
				}
			}
		}
	}

	total := len(objs)
	end := from + size
	if end > total {
		end = total
	}
	if from < 0 {
		from = 0
	}
	if from > end {
		from = end
	}
	c.JSON(http.StatusOK, gin.H{"objects": objs[from:end], "total": total, "time": time.Now().Format(time.RFC3339)})
}

func scopeOf(name string) string {
	if strings.HasPrefix(name, "@") {
		return strings.SplitN(name, "/", 2)[0][1:]
	}
	return "unscoped"
}
