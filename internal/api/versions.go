package api

import (
	"fmt"
	"net/http"
	"net/url"
	"time"

	"github.com/gin-gonic/gin"
)

// GetPackageVersions NOTE: 版本与索引相关接口集中此处，保证元数据生成逻辑清晰可查。
func (h *Handler) GetPackageVersions(c *gin.Context) {
	name := c.Param("name")
	md, err := h.vc.BuildPackageMetadata(c.Request.Context(), name)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

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
		versionsWithStatus = append(versionsWithStatus, gin.H{"name": v.Name, "version": v.Version, "dist": v.Dist, "syncStatus": syncStatus, "syncTime": syncTime})
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
		distTagsWithStatus[tag] = gin.H{"version": version, "syncStatus": syncStatus, "syncTime": syncTime}
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

	syncState := h.syncManager.GetSyncState()
	pkgKey := fmt.Sprintf("%s@%s", name, v)
	syncStatus := "pending"
	var syncTime time.Time
	if state, exists := syncState.PackageStates[pkgKey]; exists {
		syncStatus = state.SyncStatus
		syncTime = state.SyncTime
	}
	c.JSON(http.StatusOK, gin.H{"name": name, "version": v, "syncStatus": syncStatus, "syncTime": syncTime})
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
			if h.config.CDNEnabled && h.config.CDNEndpoint != "" {
				s3Key := h.s3Client.GetPackageKey(data.Packages[i].Name, data.Packages[i].Version)
				data.Packages[i].Dist.Tarball = h.s3Client.BuildCDNURL(s3Key)
			} else {
				data.Packages[i].Dist.Tarball = fmt.Sprintf("http://%s/download/%s/%s", host, data.Packages[i].Version, url.PathEscape(data.Packages[i].Name))
			}
			if st.Size > 0 {
				data.Packages[i].Dist.Size = st.Size
			}
		}
	}
	c.JSON(http.StatusOK, data)
}
