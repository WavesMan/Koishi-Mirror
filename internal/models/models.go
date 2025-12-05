package models

import (
	"time"
)

// Package 包信息
type Package struct {
	Name        string    `json:"name"`
	Version     string    `json:"version"`
	Description string    `json:"description,omitempty"`
	Author      string    `json:"author,omitempty"`
	Dist        struct {
		Tarball string `json:"tarball"`
		Size    int64  `json:"size,omitempty"`
		Shasum  string `json:"shasum,omitempty"`
	} `json:"dist"`
	SyncStatus  string    `json:"syncStatus"` // "pending", "syncing", "success", "failed"
	SyncTime    time.Time `json:"syncTime,omitempty"`
	RetryCount  int       `json:"retryCount,omitempty"`
}

// DataSource 数据源结构
type DataSource struct {
	Total    int       `json:"total"`
	Packages []Package `json:"packages"`
}

// PackageState 单个包的同步状态
type PackageState struct {
	Version     string    `json:"version"`
	SyncStatus  string    `json:"syncStatus"`
	SyncTime    time.Time `json:"syncTime"`
	RetryCount  int       `json:"retryCount"`
	Size        int64     `json:"size,omitempty"`
}

// SyncState 同步状态
type SyncState struct {
	LastSyncTime    time.Time                `json:"lastSyncTime"`
	TotalPackages   int                      `json:"totalPackages"`
	SyncedPackages  int                      `json:"syncedPackages"`
	FailedPackages  int                      `json:"failedPackages"`
	PackageStates   map[string]PackageState  `json:"packageStates"`
}

// PackageStat 包统计信息
type PackageStat struct {
	VersionCount int     `json:"versionCount"`
	TotalSize    int64   `json:"totalSize"`
	MaxSize      int64   `json:"maxSize"`
	MinSize      int64   `json:"minSize"`
	AvgSize      float64 `json:"avgSize"`
}

// StorageStats 存储统计
type StorageStats struct {
	TotalSize     int64               `json:"totalSize"`
	PackageCount  int                 `json:"packageCount"`
	VersionCount  int                 `json:"versionCount"`
	PackageStats  map[string]PackageStat `json:"packageStats"`
}

// MirrorStatus 镜像状态
type MirrorStatus struct {
    LastSyncTime   time.Time `json:"lastSyncTime"`
    TotalPackages  int       `json:"totalPackages"`
    SyncedPackages int       `json:"syncedPackages"`
    FailedPackages int       `json:"failedPackages"`
    StorageSize    int64     `json:"storageSize"`
    S3Bucket       string    `json:"s3Bucket"`
    DataSourceURL  string    `json:"dataSourceURL"`
    ICPEnabled     bool      `json:"icpEnabled"`
    ICPRecord      string    `json:"icpRecord"`
    ICPUrl         string    `json:"icpUrl"`
    SecurityRecord string    `json:"securityRecord"`
    SecurityUrl    string    `json:"securityUrl"`
    StatusBreakdown map[string]int `json:"statusBreakdown,omitempty"`
}

// PackageDetail 包详情
type PackageDetail struct {
	Name        string    `json:"name"`
	Description string    `json:"description,omitempty"`
	Author      string    `json:"author,omitempty"`
	Versions    []struct {
		Version     string    `json:"version"`
		Dist        struct {
			Tarball string `json:"tarball"`
			Size    int64  `json:"size,omitempty"`
			Shasum  string `json:"shasum,omitempty"`
		} `json:"dist"`
		SyncStatus  string    `json:"syncStatus"`
		SyncTime    time.Time `json:"syncTime,omitempty"`
	} `json:"versions"`
}

// PackageListResponse 包列表响应
type PackageListResponse struct {
	Total    int       `json:"total"`
	Packages []Package `json:"packages"`
	Page     int       `json:"page"`
	PageSize int       `json:"pageSize"`
}
