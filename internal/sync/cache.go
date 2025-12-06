package sync

import (
    "context"
    "os"
    "strings"
)

// cleanCaches 清理数据源本地缓存与临时tgz文件，并做日志保留策略。
// 为什么：长期运行会累积大量临时文件与冗余缓存，影响磁盘与性能，需要周期清理。
func (sm *SyncManager) cleanCaches(ctx context.Context) {
    if sm.ds != nil { sm.ds.ClearCache(ctx) }
    tmp := os.TempDir()
    if entries, err := os.ReadDir(tmp); err == nil {
        for _, e := range entries {
            n := e.Name()
            if strings.HasPrefix(n, "npm-") && strings.HasSuffix(n, ".tgz") {
                _ = os.Remove(tmp + string(os.PathSeparator) + n)
            }
        }
    }
    if sm.store != nil { _ = sm.store.PurgeLogsOlderThan(ctx, 30) }
}
