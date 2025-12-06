package api

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
)

// LogTest NOTE: 同步与后台任务接口集中管理，统一触发入口与回退策略。
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

func (h *Handler) BootSync(c *gin.Context) {
    if !h.syncManager.TryStartJob("boot") {
        c.JSON(http.StatusConflict, gin.H{"error": "任务正在运行"})
        return
    }
    go func() {
        defer h.syncManager.FinishJob("boot")
        _ = h.syncManager.BootSync(context.Background())
    }()
    c.JSON(http.StatusOK, gin.H{"message": "已触发初始化同步"})
}

func (h *Handler) TriggerSync(c *gin.Context) {
    if !h.syncManager.TryStartJob("pull") {
        c.JSON(http.StatusConflict, gin.H{"error": "任务正在运行"})
        return
    }
    go func() {
        defer h.syncManager.FinishJob("pull")
        if err := h.syncManager.Pull(context.Background()); err != nil {
            fmt.Printf("手动触发同步失败: %v\n", err)
        }
    }()
    c.JSON(http.StatusOK, gin.H{"message": "同步任务已触发", "time": time.Now().Format(time.RFC3339)})
}

func (h *Handler) RetryFailed(c *gin.Context) {
	go h.syncManager.RetryFailedNow(context.Background())
	c.JSON(http.StatusOK, gin.H{"message": "已触发失败重试"})
}

func (h *Handler) AdvisoriesBulk(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"advisories": []interface{}{}, "objects": []interface{}{}})
}
