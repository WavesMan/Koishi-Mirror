package api

import (
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"npm-mirror/internal/models"
)

// GetPackages NOTE: 领域拆分：包列表与详情相关接口集中此处，保证职责清晰、便于维护与扩展。
func (h *Handler) GetPackages(c *gin.Context) {
	page := 1
	pageSize := 50
	if v := c.DefaultQuery("page", "1"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			page = n
		}
	}
	if v := c.DefaultQuery("pageSize", "50"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			pageSize = n
		}
	}
	search := c.Query("search")

	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 100 {
		pageSize = 50
	}

	data, err := h.ds.Get(c.Request.Context())
	if err != nil {
		c.JSON(500, gin.H{"error": "拉取数据源失败"})
		return
	}

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

	if h.store != nil {
		for i := range pagedPackages {
			if versions, err := h.store.GetPackageVersions(c.Request.Context(), pagedPackages[i].Name); err == nil {
				for _, v := range versions {
					if v.Version == pagedPackages[i].Version {
						pagedPackages[i].SyncStatus = v.SyncStatus
						pagedPackages[i].SyncTime = v.SyncTime
						if v.Dist.Size > 0 {
							pagedPackages[i].Dist.Size = v.Dist.Size
						}
						break
					}
				}
			}
			if pagedPackages[i].SyncStatus == "" {
				pagedPackages[i].SyncStatus = "pending"
			}
		}
	} else {
		syncState := h.syncManager.GetSyncState()
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
	}

	response := models.PackageListResponse{Total: len(packages), Packages: pagedPackages, Page: page, PageSize: pageSize}
	c.JSON(http.StatusOK, response)
}

func (h *Handler) GetPackageDetail(c *gin.Context) {
	name := c.Param("name")
	if strings.HasPrefix(name, "/") {
		name = name[1:]
	}
	if u, err := url.PathUnescape(name); err == nil {
		name = u
	}
	name = strings.ReplaceAll(name, "%2F", "/")
	name = strings.ReplaceAll(name, "%2f", "/")

	data, err := h.ds.Get(c.Request.Context())
	if err != nil {
		c.JSON(500, gin.H{"error": "拉取数据源失败"})
		return
	}

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

			var syncStatus string
			var syncTime time.Time
			var size int64

			if h.store != nil {
				if versions, err := h.store.GetPackageVersions(c.Request.Context(), pkg.Name); err == nil {
					for _, v := range versions {
						if v.Version == pkg.Version {
							syncStatus = v.SyncStatus
							syncTime = v.SyncTime
							if v.Dist.Size > 0 {
								size = v.Dist.Size
							}
							break
						}
					}
				}
			}

			if syncStatus == "" {
				pkgKey := fmt.Sprintf("%s@%s", pkg.Name, pkg.Version)
				syncState := h.syncManager.GetSyncState()
				if state, exists := syncState.PackageStates[pkgKey]; exists {
					syncStatus = state.SyncStatus
					syncTime = state.SyncTime
					if state.Size > 0 {
						size = state.Size
					}
				} else {
					syncStatus = "pending"
				}
			}

			if size > 0 {
				pkg.Dist.Size = size
			}

			version := struct {
				Version string `json:"version"`
				Dist    struct {
					Tarball string `json:"tarball"`
					Size    int64  `json:"size,omitempty"`
					Shasum  string `json:"shasum,omitempty"`
				} `json:"dist"`
				SyncStatus string    `json:"syncStatus"`
				SyncTime   time.Time `json:"syncTime,omitempty"`
			}{Version: pkg.Version, SyncStatus: syncStatus, SyncTime: syncTime}

			if syncStatus == "success" && h.config.CDNEnabled && h.config.CDNEndpoint != "" {
				s3Key := h.s3Client.GetPackageKey(pkg.Name, pkg.Version)
				version.Dist.Tarball = h.s3Client.BuildCDNURL(s3Key)
			} else {
				version.Dist.Tarball = fmt.Sprintf("/download/%s/%s", pkg.Version, url.PathEscape(pkg.Name))
			}
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
