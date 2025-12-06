package sync

import (
    "bufio"
    "context"
    "fmt"
    "io"
    "net/http"
    "net/url"
    "os"

    "npm-mirror/internal/models"
)

// syncPackage 同步单个包：下载 → 净化（修复 chunked 污染/定位 gzip 魔数）→ 上传 → 校验 → 持久化。
// 为什么：上游网络服务可能返回分块编码或前后部污染，需规范化为纯 gzip tarball 后再入库与上云。
// WARNING: 该流程依赖外部网络与对象存储，建议结合降级/重试/观测以提升鲁棒性。
func (sm *SyncManager) syncPackage(ctx context.Context, pkg models.Package) error {
    pkgKey := fmt.Sprintf("%s@%s", pkg.Name, pkg.Version)
    sm.updatePackageState(pkgKey, "syncing", 0)

    tempFile, err := os.CreateTemp("", "npm-*.tgz")
    if err != nil {
        sm.updatePackageState(pkgKey, "failed", 0)
        return fmt.Errorf("创建临时文件失败: %v", err)
    }
    defer os.Remove(tempFile.Name())

    if sm.logger != nil {
        sm.logger.Debug(
            "sync",
            "download_start",
            map[string]interface{}{
                "name":    pkg.Name,
                "version": pkg.Version,
                "url":     pkg.Dist.Tarball,
            },
        )
    }
    resp, err := http.Get(pkg.Dist.Tarball)
    if err != nil {
        sm.updatePackageState(pkgKey, "failed", 0)
        if sm.logger != nil {
            sm.logger.Error(
                "sync",
                "download_error",
                map[string]interface{}{
                    "name":    pkg.Name,
                    "version": pkg.Version,
                    "error":   err.Error(),
                },
            )
        }
        return fmt.Errorf("下载包失败: %v", err)
    }
    defer resp.Body.Close()

    fileSize, err := io.Copy(tempFile, resp.Body)
    if err != nil {
        sm.updatePackageState(pkgKey, "failed", 0)
        if sm.logger != nil {
            sm.logger.Error(
                "sync",
                "write_temp_error",
                map[string]interface{}{
                    "name":    pkg.Name,
                    "version": pkg.Version,
                    "error":   err.Error(),
                },
            )
        }
        return fmt.Errorf("写入临时文件失败: %v", err)
    }
    if sm.logger != nil {
        sm.logger.Debug(
            "sync",
            "download_done",
            map[string]interface{}{
                "name":    pkg.Name,
                "version": pkg.Version,
                "bytes":   fileSize,
            },
        )
    }

    if _, err := tempFile.Seek(0, 0); err != nil {
        sm.updatePackageState(pkgKey, "failed", 0)
        return fmt.Errorf("重置文件指针失败: %v", err)
    }

    if _, err := tempFile.Seek(0, 0); err != nil {
        sm.updatePackageState(pkgKey, "failed", 0)
        return fmt.Errorf("重置文件指针失败: %v", err)
    }
    br := bufio.NewReader(tempFile)
    head, _ := br.Peek(64)
    gzipIdx := -1
    for i := 0; i+1 < len(head); i++ {
        if head[i] == 0x1F && head[i+1] == 0x8B { gzipIdx = i; break }
    }
    info, _ := tempFile.Stat()
    total := info.Size()
    suffix := int64(0)
    if total >= 5 {
        if _, err := tempFile.Seek(total-5, 0); err == nil {
            tail := make([]byte, 5)
            if _, err := io.ReadFull(tempFile, tail); err == nil {
                if tail[0] == '0' && tail[1] == '\r' && tail[2] == '\n' && tail[3] == '\r' && tail[4] == '\n' { suffix = 5 }
            }
        }
    }
    // NOTE: 若 gzip 魔数不在起始位置或尾部存在 chunk 终止符，则截取有效区间生成干净对象。
    if gzipIdx != 0 || suffix > 0 {
        start := int64(0)
        if gzipIdx > 0 { start = int64(gzipIdx) }
        length := total - suffix - start
        if length <= 0 {
            sm.updatePackageState(pkgKey, "failed", 0)
            return fmt.Errorf("清理后长度异常: total=%d start=%d suffix=%d", total, start, suffix)
        }
        cleaned, err := os.CreateTemp("", "npm-clean-*.tgz")
        if err != nil {
            sm.updatePackageState(pkgKey, "failed", 0)
            return fmt.Errorf("创建清理临时文件失败: %v", err)
        }
        defer os.Remove(cleaned.Name())
        if _, err := tempFile.Seek(start, 0); err != nil {
            sm.updatePackageState(pkgKey, "failed", 0)
            return fmt.Errorf("定位清理起点失败: %v", err)
        }
        n, err := io.CopyN(cleaned, tempFile, length)
        if err != nil && err != io.EOF {
            sm.updatePackageState(pkgKey, "failed", 0)
            return fmt.Errorf("复制清理数据失败: %v", err)
        }
        if _, err := cleaned.Seek(0, 0); err != nil {
            sm.updatePackageState(pkgKey, "failed", 0)
            return fmt.Errorf("重置清理文件指针失败: %v", err)
        }
        tempFile.Close()
        tempFile = cleaned
        fileSize = n
        if sm.logger != nil {
            sm.logger.Info(
                "sync",
                "clean_chunk_fix",
                map[string]interface{}{
                    "name":    pkg.Name,
                    "version": pkg.Version,
                    "start":   start,
                    "suffix":  suffix,
                    "bytes":   n,
                },
            )
        }
    } else {
        if _, err := tempFile.Seek(0, 0); err != nil {
            sm.updatePackageState(pkgKey, "failed", 0)
            return fmt.Errorf("重置文件指针失败: %v", err)
        }
    }

    s3Key := sm.s3Client.GetPackageKey(pkg.Name, pkg.Version)
    if sm.logger != nil {
        sm.logger.Debug(
            "s3",
            "upload_start",
            map[string]interface{}{
                "key":  s3Key,
                "size": fileSize,
            },
        )
    }
    if err := sm.s3Client.UploadFile(ctx, s3Key, tempFile, "application/gzip", fileSize); err != nil {
        sm.updatePackageState(pkgKey, "failed", 0)
        if sm.logger != nil {
            sm.logger.Error(
                "s3",
                "upload_error",
                map[string]interface{}{
                    "key":   s3Key,
                    "error": err.Error(),
                },
            )
        }
        return fmt.Errorf("上传S3失败: %v", err)
    }
    if sm.logger != nil {
        sm.logger.Debug(
            "s3",
            "upload_done",
            map[string]interface{}{
                "key":  s3Key,
                "size": fileSize,
            },
        )
    }

    if info, err := sm.s3Client.GetFileInfo(ctx, s3Key); err == nil {
        var actual int64
        if info.Size != nil { actual = *info.Size }
        if actual != fileSize {
            if sm.logger != nil {
                sm.logger.Warn(
                    "s3",
                    "size_mismatch_after_upload",
                    map[string]interface{}{
                        "key":   s3Key,
                        "local": fileSize,
                        "s3":    actual,
                    },
                )
            }
            sm.updatePackageState(pkgKey, "failed", actual)
            return fmt.Errorf("S3对象大小不一致: 本地=%d S3=%d", fileSize, actual)
        }
    }
    _ = os.Remove(tempFile.Name())

    pkg.Dist.Size = fileSize
    if err := sm.savePackageMetadata(ctx, pkg); err != nil {
        if sm.logger != nil {
            sm.logger.Error(
                "sync",
                "save_meta_error",
                map[string]interface{}{
                    "name":    pkg.Name,
                    "version": pkg.Version,
                    "error":   err.Error(),
                },
            )
        } else {
            fmt.Printf("保存包元数据失败 %s@%s: %v\n", pkg.Name, pkg.Version, err)
        }
    }

    sm.updatePackageState(pkgKey, "success", fileSize)
    if sm.logger != nil {
        sm.logger.Info(
            "sync",
            "同步成功",
            map[string]interface{}{
                "name":    pkg.Name,
                "version": pkg.Version,
                "key":     s3Key,
                "size":    fileSize,
            },
        )
    } else {
        fmt.Printf("同步成功: %s@%s -> S3:%s (大小: %d bytes)\n", pkg.Name, pkg.Version, s3Key, fileSize)
    }

    if sm.store != nil {
        _ = sm.store.UpsertPackageVersion(ctx, pkg.Name, pkg.Description, pkg.Author, pkg)
        _ = sm.store.UpdateVersionStatus(ctx, pkg.Name, pkg.Version, "success", fileSize)
    }

    sm.refreshCachePipeline(ctx, pkg)
    return nil
}

// refreshCachePipeline 写入少量热点键到缓存。
// 为什么：减少页面/API的回源与计算，提升最终用户的响应速度。
func (sm *SyncManager) refreshCachePipeline(ctx context.Context, pkg models.Package) {
    if sm.rcache == nil { return }
    kv := map[string]string{
        "pkg:synced:" + pkg.Name + "@" + pkg.Version: "success",
        "tar:size:" + pkg.Name + "@" + pkg.Version: fmt.Sprintf("%d", pkg.Dist.Size),
        "tar:url:" + pkg.Name + "@" + pkg.Version: "/download/" + pkg.Version + "/" + url.PathEscape(pkg.Name),
    }
    _ = sm.rcache.SetMany(ctx, kv)
}
