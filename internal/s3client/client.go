package s3client

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"strings"
	"time"

	cos "github.com/tencentyun/cos-go-sdk-v5"
	"npm-mirror/config"
	"npm-mirror/internal/logging"
)

// Client TENCENT_COS客户端
type Client struct {
	client *cos.Client
	bucket string
	prefix string
	config *config.Config
	logger *logging.Logger
}

// New 创建新的TENCENT_COS客户端
func New(cfg *config.Config) (*Client, error) {
	if cfg.TencentCosendpoint == "" {
		return nil, fmt.Errorf("TENCENT_COS endpoint is required")
	}
	u, err := url.Parse(cfg.TencentCosendpoint)
	if err != nil {
		return nil, fmt.Errorf("invalid endpoint: %v", err)
	}
	host := strings.ToLower(u.Host)
	isMyQcloud := strings.Contains(host, "myqcloud.com")
	isCosService := strings.HasPrefix(host, "cos.") && isMyQcloud
	var bucketURL *url.URL
	if isCosService {
		b := fmt.Sprintf("https://%s.cos.%s.myqcloud.com", cfg.TencentCosbucket, cfg.TencentCosregion)
		bucketURL, _ = url.Parse(b)
	} else {
		bucketURL = u
	}
	base := &cos.BaseURL{BucketURL: bucketURL}
	cli := cos.NewClient(base, &http.Client{
		Transport: &cos.AuthorizationTransport{
			SecretID:  cfg.TencentCosaccesskey,
			SecretKey: cfg.TencentCossecretkey,
		},
	})
	return &Client{
		client: cli,
		bucket: cfg.TencentCosbucket,
		prefix: strings.ReplaceAll(cfg.TencentCosprefix, "\\", "/"),
		config: cfg,
	}, nil
}

func (c *Client) SetLogger(l *logging.Logger) { c.logger = l }

type Object struct {
	Key          *string
	Size         *int64
	LastModified *time.Time
	ETag         *string
}

// UploadFile 上传文件到TENCENT_COS
func (c *Client) UploadFile(ctx context.Context, key string, reader io.Reader, contentType string, contentLength int64) error {
	if !strings.HasPrefix(key, c.prefix) {
		key = path.Join(c.prefix, key)
	}
	key = strings.ReplaceAll(key, "\\", "/")
	if c.logger != nil {
		c.logger.Debug("fuck_tx", "put_start", map[string]interface{}{"op": "PutObject", "bucket": c.bucket, "key": key, "content_type": contentType, "content_length": contentLength})
	}
	opt := &cos.ObjectPutOptions{ObjectPutHeaderOptions: &cos.ObjectPutHeaderOptions{ContentType: contentType}}
	_, err := c.client.Object.Put(ctx, key, reader, opt)
	if err != nil {
		if c.logger != nil {
			c.logger.Error("fuck_tx", "request", map[string]interface{}{"op": "PutObject", "key": key, "error": err.Error()})
		}
		return err
	}
	if c.logger != nil {
		c.logger.Info("fuck_tx", "put_done", map[string]interface{}{"op": "PutObject", "bucket": c.bucket, "key": key})
	}
	return nil
}

// DownloadFile 从TENCENT_COS下载文件
func (c *Client) DownloadFile(ctx context.Context, key string) (io.ReadCloser, error) {
	if !strings.HasPrefix(key, c.prefix) {
		key = path.Join(c.prefix, key)
	}
	key = strings.ReplaceAll(key, "\\", "/")
	if c.logger != nil {
		c.logger.Debug("fuck_tx", "get_start", map[string]interface{}{"op": "GetObject", "bucket": c.bucket, "key": key})
	}
	resp, err := c.client.Object.Get(ctx, key, nil)
	if err != nil {
		if c.logger != nil {
			c.logger.Error("fuck_tx", "request", map[string]interface{}{"op": "GetObject", "key": key, "error": err.Error()})
		}
		return nil, err
	}
	return resp.Body, nil
}

// GetFileInfo 获取文件信息
func (c *Client) GetFileInfo(ctx context.Context, key string) (*Object, error) {
	if !strings.HasPrefix(key, c.prefix) {
		key = path.Join(c.prefix, key)
	}
	key = strings.ReplaceAll(key, "\\", "/")
	if c.logger != nil {
		c.logger.Debug("fuck_tx", "head_start", map[string]interface{}{"op": "HeadObject", "bucket": c.bucket, "key": key})
	}
	resp, err := c.client.Object.Head(ctx, key, nil)
	if err != nil {
		if c.logger != nil {
			c.logger.Error("fuck_tx", "request", map[string]interface{}{"op": "HeadObject", "key": key, "error": err.Error()})
		}
		return nil, err
	}
	var sizePtr *int64
	if v := resp.Header.Get("Content-Length"); v != "" {
		if n, e := strconv.ParseInt(v, 10, 64); e == nil {
			sizePtr = &n
		}
	}
	var etagPtr *string
	if v := resp.Header.Get("ETag"); v != "" {
		vv := strings.TrimSpace(v)
		etagPtr = &vv
	}
	var lmPtr *time.Time
	if v := resp.Header.Get("Last-Modified"); v != "" {
		if t, e := http.ParseTime(v); e == nil {
			lm := t
			lmPtr = &lm
		}
	}
	k := key
	return &Object{Key: &k, Size: sizePtr, LastModified: lmPtr, ETag: etagPtr}, nil
}

// ListObjects 列出TENCENT_COS中的对象
func (c *Client) ListObjects(ctx context.Context, prefix string, delimiter string) ([]Object, error) {
	fullPrefix := c.prefix
	if prefix != "" {
		fullPrefix = path.Join(c.prefix, prefix)
	}
	fullPrefix = strings.ReplaceAll(fullPrefix, "\\", "/")
	var out []Object
	opt := &cos.BucketGetOptions{Prefix: fullPrefix, Delimiter: delimiter}
	resp, _, err := c.client.Bucket.Get(ctx, opt)
	if err != nil {
		if c.logger != nil {
			c.logger.Error("fuck_tx", "request", map[string]interface{}{"op": "ListObjects", "key": fullPrefix, "error": err.Error()})
		}
		return nil, err
	}
	for _, o := range resp.Contents {
		k := o.Key
		var szPtr *int64
		sz := int64(o.Size)
		szPtr = &sz
		var lmPtr *time.Time
		if o.LastModified != "" {
			if t, e := time.Parse(time.RFC3339, o.LastModified); e == nil {
				lm := t
				lmPtr = &lm
			}
		}
		var etagPtr *string
		if o.ETag != "" {
			v := o.ETag
			etagPtr = &v
		}
		out = append(out, Object{Key: &k, Size: szPtr, LastModified: lmPtr, ETag: etagPtr})
	}
	return out, nil
}

// DeleteObject 删除TENCENT_COS中的对象
func (c *Client) DeleteObject(ctx context.Context, key string) error {
	if !strings.HasPrefix(key, c.prefix) {
		key = path.Join(c.prefix, key)
	}
	key = strings.ReplaceAll(key, "\\", "/")
	if c.logger != nil {
		c.logger.Debug("fuck_tx", "delete_start", map[string]interface{}{"op": "DeleteObject", "bucket": c.bucket, "key": key})
	}
	_, err := c.client.Object.Delete(ctx, key)
	if err != nil {
		if c.logger != nil {
			c.logger.Error("fuck_tx", "request", map[string]interface{}{"op": "DeleteObject", "key": key, "error": err.Error()})
		}
		return err
	}
	if c.logger != nil {
		c.logger.Info("fuck_tx", "delete_done", map[string]interface{}{"op": "DeleteObject", "bucket": c.bucket, "key": key})
	}
	return nil
}

// GetPresignedURL 生成预签名下载链接
func (c *Client) GetPresignedURL(ctx context.Context, key string, lifetime time.Duration) (string, error) {
	if !strings.HasPrefix(key, c.prefix) {
		key = path.Join(c.prefix, key)
	}
	key = strings.ReplaceAll(key, "\\", "/")
	u, err := c.client.Object.GetPresignedURL2(ctx, http.MethodGet, key, lifetime, nil)
	if err != nil {
		return "", err
	}
	return u.String(), nil
}

func (c *Client) BuildCDNURL(key string) string {
	if !strings.HasPrefix(key, c.prefix) {
		key = path.Join(c.prefix, key)
	}
	key = strings.ReplaceAll(key, "\\", "/")
	ep := strings.TrimRight(c.config.CDNEndpoint, "/")
	if ep == "" {
		return ""
	}
	return ep + "/" + key
}

// NormalizePackageName 将 npm 包名规范化为适合 TENCENT_COS 路径的形式（移除 @scope 并用 - 代替 /）
func NormalizePackageName(packageName string) string {
	n := strings.TrimPrefix(packageName, "@")
	n = strings.ReplaceAll(n, "/", "-")
	return n
}

// GetPackageKey 获取包在TENCENT_COS中的key
func (c *Client) GetPackageKey(packageName, version string) string {
	base := NormalizePackageName(packageName)
	return strings.ReplaceAll(
		path.Join(
			c.prefix,
			base,
			version,
			fmt.Sprintf("%s-%s.tgz", base, version)),
		"\\", "/",
	)
}

// GetPackageMetaKey 获取包元数据在TENCENT_COS中的key
func (c *Client) GetPackageMetaKey(packageName string) string {
	return strings.ReplaceAll(
		path.Join(
			c.prefix,
			packageName,
			"metadata.json",
		),
		"\\", "/",
	)
}

func (c *Client) GetSyncStateKey() string {
	return strings.ReplaceAll(
		path.Join(
			c.prefix,
			"_index",
			"sync_state.json",
		),
		"\\", "/",
	)
}
