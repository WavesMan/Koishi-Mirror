package api

import (
    "fmt"
    "net/http"
    "net/url"
    "strconv"
    "strings"
    "time"

    "github.com/gin-gonic/gin"
    "npm-mirror/internal/proxy"
)

// NOTE: 领域拆分：registry 相关接口集中于此，降低 handler.go 体积并明确职责边界。

func (h *Handler) GetRegistry(c *gin.Context) {
    c.JSON(http.StatusOK, gin.H{"registry": fmt.Sprintf("http://%s/", c.Request.Host)})
}

// 文档注释（为什么做）：优先返回镜像的元数据；当镜像缺失或构建失败时，安全透传至上游，保证 npm 客户端可用性。
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
        resp, e := h.upstream.ProxyMetadata(raw)
        if e != nil {
            c.JSON(http.StatusBadGateway, gin.H{"error": "上游服务不可用"})
            return
        }
        defer resp.Body.Close()

        if err := proxy.StreamResponse(resp, c.Writer); err != nil {
            c.JSON(http.StatusInternalServerError, gin.H{"error": "代理响应失败"})
        }
        return
    }

    syncState := h.syncManager.GetSyncState()
    host := c.Request.Host

    versions := map[string]gin.H{}
    for _, v := range md.Versions {
        key := v.Name + "@" + v.Version
        tar := v.Dist.Tarball
        if st, ok := syncState.PackageStates[key]; ok && st.SyncStatus == "success" {
            if h.config.CDNEnabled && h.config.CDNEndpoint != "" {
                s3Key := h.s3Client.GetPackageKey(v.Name, v.Version)
                tar = h.s3Client.BuildCDNURL(s3Key)
            } else {
                tar = fmt.Sprintf("http://%s/download/%s/%s", host, v.Version, url.PathEscape(v.Name))
            }
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

// NOTE: 回退策略说明：浏览器请求回退到 SPA；安全审计与搜索接口提供最小可用响应，避免阻塞开发者流程。
func (h *Handler) RegistryFallback(c *gin.Context) {
    p := c.Request.URL.Path

    if strings.Contains(c.Request.Header.Get("Accept"), "text/html") {
        c.File("./ui/dist/index.html")
        return
    }

    if strings.HasPrefix(p, "/-/npm/v1/security/advisories/bulk") {
        c.JSON(http.StatusOK, gin.H{"advisories": []interface{}{}, "objects": []interface{}{}})
        return
    }
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
        end := from + size
        if end > total { end = total }
        if from < 0 { from = 0 }
        if from > end { from = end }
        c.JSON(http.StatusOK, gin.H{
            "objects": objs[from:end],
            "total":   total,
            "time":    time.Now().Format(time.RFC3339),
        })
        return
    }

    raw := strings.TrimPrefix(p, "/")
    if raw == "" || strings.HasPrefix(raw, "api/") || strings.HasPrefix(raw, "download/") {
        c.JSON(http.StatusNotFound, gin.H{"error": "未匹配的路由"})
        return
    }
    if u, err := url.PathUnescape(raw); err == nil { raw = u }
    raw = strings.ReplaceAll(raw, "%2F", "/")
    raw = strings.ReplaceAll(raw, "%2f", "/")

    md, err := h.vc.BuildPackageMetadata(c.Request.Context(), raw)
    if err != nil {
        resp, e := h.upstream.ProxyMetadata(raw)
        if e != nil {
            c.JSON(http.StatusBadGateway, gin.H{"error": "上游服务不可用"})
            return
        }
        defer resp.Body.Close()
        if err := proxy.StreamResponse(resp, c.Writer); err != nil {
            c.JSON(http.StatusInternalServerError, gin.H{"error": "代理响应失败"})
        }
        return
    }

    host := c.Request.Host
    versions := map[string]gin.H{}
    for _, v := range md.Versions {
        key := v.Name + "@" + v.Version
        tar := v.Dist.Tarball
        isSynced := false
        if h.store != nil {
            if dbVersions, err := h.store.GetPackageVersions(c.Request.Context(), v.Name); err == nil {
                for _, dbv := range dbVersions {
                    if dbv.Version == v.Version && dbv.SyncStatus == "success" {
                        isSynced = true
                        break
                    }
                }
            }
        }
        if !isSynced {
            syncState := h.syncManager.GetSyncState()
            if st, ok := syncState.PackageStates[key]; ok && st.SyncStatus == "success" {
                isSynced = true
            }
        }
        if isSynced {
            if h.config.CDNEnabled && h.config.CDNEndpoint != "" {
                s3Key := h.s3Client.GetPackageKey(v.Name, v.Version)
                tar = h.s3Client.BuildCDNURL(s3Key)
            } else {
                tar = fmt.Sprintf("http://%s/download/%s/%s", host, v.Version, url.PathEscape(v.Name))
            }
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
    resp := gin.H{"name": md.Name, "dist-tags": md.DistTags, "versions": versions}
    c.JSON(http.StatusOK, resp)
}
