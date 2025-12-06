package api

import (
    "fmt"
    "net/http"
    "net/url"
    "strings"
    "time"

    "github.com/gin-gonic/gin"
    "npm-mirror/internal/proxy"
)

// NOTE: 下载路径解耦：此文件仅处理 tarball 下载与回源逻辑，避免业务散落在 handler.go。
// 为什么：优先重定向到 CDN（节省带宽/提升体验），否则使用短期签名链接，兼顾安全与可用。
func (h *Handler) DownloadPackage(c *gin.Context) {
    name := c.Param("name")
    if strings.HasPrefix(name, "/") { name = name[1:] }
    if u, err := url.PathUnescape(name); err == nil { name = u }
    name = strings.ReplaceAll(name, "%2F", "/")
    name = strings.ReplaceAll(name, "%2f", "/")
    version := c.Param("version")

    var isSynced bool
    if h.store != nil {
        if versions, err := h.store.GetPackageVersions(c.Request.Context(), name); err == nil {
            for _, v := range versions {
                if v.Version == version && v.SyncStatus == "success" { isSynced = true; break }
            }
        }
    }
    if !isSynced {
        syncState := h.syncManager.GetSyncState()
        pkgKey := fmt.Sprintf("%s@%s", name, version)
        state, exists := syncState.PackageStates[pkgKey]
        if !exists || state.SyncStatus != "success" {
            tarballURL, err := h.upstream.GetTarballURL(name, version)
            if err != nil { c.JSON(http.StatusNotFound, gin.H{"error": "包不存在或未同步完成"}); return }
            resp, err := h.upstream.ProxyTarball(tarballURL)
            if err != nil { c.JSON(http.StatusBadGateway, gin.H{"error": "上游服务不可用"}); return }
            defer resp.Body.Close()
            if err := proxy.StreamResponse(resp, c.Writer); err != nil { c.JSON(http.StatusInternalServerError, gin.H{"error": "代理响应失败"}) }
            return
        }
    }

    s3Key := h.s3Client.GetPackageKey(name, version)
    if h.config.CDNEnabled && h.config.CDNEndpoint != "" {
        endpoint := strings.TrimRight(h.config.CDNEndpoint, "/")
        cdnURL := endpoint + "/" + s3Key
        c.Redirect(http.StatusFound, cdnURL)
        return
    }
    downloadURL, err := h.s3Client.GetPresignedURL(c.Request.Context(), s3Key, 15*time.Minute)
    if err != nil { c.JSON(http.StatusInternalServerError, gin.H{"error": "获取下载链接失败"}); return }
    c.Redirect(http.StatusFound, downloadURL)
}
