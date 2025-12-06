package sync

import (
    "fmt"
    "sort"
    "strconv"
    "strings"

    "npm-mirror/internal/models"
)

// determinePackagesToSync 生成待同步列表，优先保留失败与可重试的 pending。
// 为什么：按可恢复性与重试上限约束，避免无效重试与资源浪费。
func (sm *SyncManager) determinePackagesToSync(packages []models.Package) []models.Package {
    sm.stateMutex.RLock()
    defer sm.stateMutex.RUnlock()

    var packagesToSync []models.Package
    for _, pkg := range packages {
        pkgKey := fmt.Sprintf("%s@%s", pkg.Name, pkg.Version)
        state, exists := sm.syncState.PackageStates[pkgKey]
        if !exists ||
            state.SyncStatus == "failed" ||
            (state.SyncStatus == "pending" && state.RetryCount < sm.config.MaxRetries) {
            packagesToSync = append(packagesToSync, pkg)
        }
    }

    if len(packagesToSync) > 1 {
        sort.SliceStable(packagesToSync, func(i, j int) bool {
            ai := strings.HasPrefix(packagesToSync[i].Name, "@koishijs/")
            aj := strings.HasPrefix(packagesToSync[j].Name, "@koishijs/")
            if ai != aj { return ai && !aj }
            return packagesToSync[i].Name < packagesToSync[j].Name
        })
    }
    return packagesToSync
}

// planPriorityAndBackfill 规划“最新版本优先 + 历史版本回填”。
// 为什么：先保证用户最关心的可用性，再分批补齐历史，平衡体验与成本。
func (sm *SyncManager) planPriorityAndBackfill(packages []models.Package) ([]models.Package, []models.Package) {
    if len(packages) == 0 { return nil, nil }
    latestMap := make(map[string]string)
    for _, p := range packages {
        v := latestMap[p.Name]
        if v == "" || compareVer(p.Version, v) > 0 {
            latestMap[p.Name] = p.Version
        }
    }
    var pri, back []models.Package
    for _, p := range packages {
        if latestMap[p.Name] == p.Version {
            pri = append(pri, p)
        } else {
            back = append(back, p)
        }
    }
    return pri, back
}

// compareVer 仅比较主流语义版本的数字部分，忽略预发布标记。
// 为什么：用于最新版本优先的粗略排序，满足镜像端的实际需求。
func compareVer(a, b string) int {
    sa := strings.Split(a, ".")
    sb := strings.Split(b, ".")
    n := len(sa)
    if len(sb) > n { n = len(sb) }
    for i := 0; i < n; i++ {
        var ai, bi int
        if i < len(sa) { ai, _ = strconv.Atoi(strings.SplitN(sa[i], "-", 2)[0]) }
        if i < len(sb) { bi, _ = strconv.Atoi(strings.SplitN(sb[i], "-", 2)[0]) }
        if ai > bi { return 1 }
        if ai < bi { return -1 }
    }
    return 0
}
