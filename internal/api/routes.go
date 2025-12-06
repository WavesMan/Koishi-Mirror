package api

import (
	"github.com/gin-gonic/gin"
	"strings"
)

// RegisterRoutes NOTE: 路由与中间件集中在此，目的在于界面与接口职责分离，降低耦合度。
// 为什么：统一缓存策略（静态资源长缓存、首页不缓存）以提升加载性能并避免 SPA 的版本混淆。
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

	r.GET("/api/status", h.GetMirrorCOSStatus)
	r.GET("/api/mirror-status", h.GetMirrorStatus)
	r.GET("/api/packages", h.GetPackages)
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

	r.Static("/assets", "./ui/dist/assets")
	r.StaticFile("/favicon.ico", "./ui/dist/favicon.ico")
	r.StaticFile("/koishi.png", "./ui/dist/koishi.png")

	r.NoRoute(h.RegistryFallback)
	r.GET("/download/:version/*name", h.DownloadPackage)
	r.GET("/index.json", h.GetIndex)
}
