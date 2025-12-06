package sync

import (
    "context"
    "net/url"
    "strings"
    "time"

    "npm-mirror/internal/models"
)

type ReconcileSummary struct {
    Matched         int      `json:"matched"`
    DBMissingS3     []string `json:"dbMissingS3"`
    S3MissingDB     []string `json:"s3MissingDb"`
    SizeMismatched  []string `json:"sizeMismatched"`
    UpdatedDB       []string `json:"updatedDb"`
    ScheduledResync []string `json:"scheduledResync"`
}

// buildS3Index 构建对象存储中的版本索引（仅关心 .tgz）。
// 为什么：快速判断缺失与一致性，避免对每个版本逐个请求元信息的高成本。
// TODO: 若对象规模很大需考虑分页与增量索引，避免一次性扫描造成压力。
func (sm *SyncManager) buildS3Index(ctx context.Context) (map[string]int64, error) {
    if sm.logger != nil { sm.logger.Debug("s3", "list_objects_start", nil) }
    idx := make(map[string]int64)
    objs, err := sm.s3Client.ListObjects(ctx, "", "")
    if err != nil { return idx, err }
    for _, o := range objs {
        k := *o.Key
        if !strings.HasSuffix(k, ".tgz") { continue }
        parts := strings.Split(k, "/")
        if len(parts) < 4 { continue }
        name := parts[len(parts)-3]
        version := parts[len(parts)-2]
        var sz int64
        if o.Size != nil { sz = *o.Size }
        idx[name+"@"+version] = sz
    }
    if sm.logger != nil {
        sm.logger.Debug(
            "s3",
            "list_objects_done",
            map[string]interface{}{
                "count": len(objs),
                "tgz":   len(idx),
            },
        )
    }
    return idx, nil
}

// Reconcile 对账 DB 与 S3：
// - DB 缺 S3：插入成功记录以消除不一致；
// - DB/S3 大小不一致：以 S3 为准更新 DB；
// - S3 缺 DB：补写 DB 以维持完整视图。
// 为什么：镜像系统的真实“可用性”由对象与索引共同决定，对账可消除用户侧的错误体验。
func (sm *SyncManager) Reconcile(ctx context.Context) (*ReconcileSummary, error) {
    if sm.store == nil { return &ReconcileSummary{}, nil }
    sum := &ReconcileSummary{}
    rows, err := sm.store.ListAllVersions(ctx)
    if err != nil { return nil, err }
    for _, r := range rows {
        name := r.Name
        version := r.Version
        size := r.Size
        key := sm.s3Client.GetPackageKey(name, version)
        info, err := sm.s3Client.GetFileInfo(ctx, key)
        if err != nil {
            if sm.logger != nil {
                sm.logger.Debug(
                    "s3",
                    "head_missing",
                    map[string]interface{}{
                        "name":    name,
                        "version": version,
                    },
                )
            }
            sum.DBMissingS3 = append(sum.DBMissingS3, name+"@"+version)
            _ = sm.store.UpdateVersionStatus(ctx, name, version, "failed", size)
            sum.ScheduledResync = append(sum.ScheduledResync, name+"@"+version)
            continue
        }
        var actual int64
        if info.Size != nil { actual = *info.Size }
        if actual != size {
            if sm.logger != nil {
                sm.logger.Debug(
                    "s3",
                    "size_mismatch",
                    map[string]interface{}{
                        "name":    name,
                        "version": version,
                        "db":      size,
                        "s3":      actual,
                    },
                )
            }
            _ = sm.store.UpdateVersionStatus(ctx, name, version, "success", actual)
            sum.SizeMismatched = append(sum.SizeMismatched, name+"@"+version)
            sum.UpdatedDB = append(sum.UpdatedDB, name+"@"+version)
        } else {
            sum.Matched++
        }
    }
    objs, err := sm.s3Client.ListObjects(ctx, "", "")
    if err == nil {
        for _, o := range objs {
            k := *o.Key
            if !strings.HasSuffix(k, ".tgz") { continue }
            parts := strings.Split(k, "/")
            if len(parts) < 4 { continue }
            name := parts[len(parts)-3]
            version := parts[len(parts)-2]
            ok, err := sm.store.HasVersion(ctx, name, version)
            if err != nil { continue }
            if !ok {
                if sm.logger != nil {
                    sm.logger.Debug(
                        "s3",
                        "db_insert_missing",
                        map[string]interface{}{
                            "name":    name,
                            "version": version,
                        },
                    )
                }
                sum.S3MissingDB = append(sum.S3MissingDB, name+"@"+version)
                pv := models.Package{Name: name, Version: version}
                pv.Dist.Tarball = "/download/" + version + "/" + url.PathEscape(name)
                if o.Size != nil { pv.Dist.Size = *o.Size }
                pv.SyncStatus = "success"
                pv.SyncTime = time.Now()
                _ = sm.store.UpsertPackageVersion(ctx, name, "", "", pv)
                var sz int64
                if o.Size != nil { sz = *o.Size }
                _ = sm.store.UpdateVersionStatus(ctx, name, version, "success", sz)
            }
        }
    }
    if sm.store != nil && sm.ds != nil {
        d, err := sm.ds.Get(ctx)
        if err == nil {
            for _, pkg := range d.Packages {
                key := sm.s3Client.GetPackageKey(pkg.Name, pkg.Version)
                info, err := sm.s3Client.GetFileInfo(ctx, key)
                if err == nil {
                    var sz int64
                    if info.Size != nil { sz = *info.Size }
                    pkg.Dist.Tarball = "/download/" + pkg.Version + "/" + url.PathEscape(pkg.Name)
                    pkg.Dist.Size = sz
                    pkg.SyncStatus = "success"
                    pkg.SyncTime = time.Now()
                    _ = sm.store.UpsertPackageVersion(ctx, pkg.Name, pkg.Description, pkg.Author, pkg)
                    _ = sm.store.UpdateVersionStatus(ctx, pkg.Name, pkg.Version, "success", sz)
                } else {
                    ok, _ := sm.store.HasVersion(ctx, pkg.Name, pkg.Version)
                    if !ok {
                        pv := models.Package{Name: pkg.Name, Version: pkg.Version}
                        pv.SyncStatus = "pending"
                        pv.SyncTime = time.Now()
                        _ = sm.store.UpsertPackageVersion(ctx, pkg.Name, pkg.Description, pkg.Author, pv)
                    } else {
                        _ = sm.store.UpdateVersionStatus(ctx, pkg.Name, pkg.Version, "failed", 0)
                    }
                }
            }
        }
    }
    return sum, nil
}
