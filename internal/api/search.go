package api

import (
    "fmt"
    "net/http"
    "strconv"
    "strings"
    "time"

    "github.com/gin-gonic/gin"
)

// NOTE: 设计解耦说明：本文件仅承载搜索相关接口，目的在于按领域拆分，降低单文件复杂度并提升可维护性。
// 文档注释（为什么做）：返回简化版搜索结果，避免引入额外依赖与复杂评分逻辑，保证接口稳定与可控。

func (h *Handler) SearchV1(c *gin.Context) {
    q := c.Request.URL.Query()
    text := q.Get("text")
    size := 20
    from := 0
    if v := q.Get("size"); v != "" {
        if n, err := strconv.Atoi(v); err == nil { size = n }
    }
    if v := q.Get("from"); v != "" {
        if n, err := strconv.Atoi(v); err == nil { from = n }
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
                            "links": gin.H{"npm": fmt.Sprintf("http://%s/%s", c.Request.Host, pkg.Name)},
                            "publisher":   gin.H{"username": "", "email": ""},
                            "maintainers": []interface{}{},
                        },
                        "score": gin.H{
                            "final": 0.0,
                            "detail": gin.H{"quality": 0.0, "popularity": 0.0, "maintenance": 0.0},
                        },
                        "searchScore": 1.0,
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
    c.JSON(http.StatusOK, gin.H{"objects": objs[from:end], "total": total, "time": time.Now().Format(time.RFC3339)})
}

// NOTE: 语义说明：scopeOf 仅做最小化的作用域解析，维持 npm 客户端兼容性，不参与复杂规范校验。
func scopeOf(name string) string {
    if strings.HasPrefix(name, "@") { return strings.SplitN(name, "/", 2)[0][1:] }
    return "unscoped"
}
