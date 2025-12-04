package s3client

import (
    "context"
    "fmt"
    "io"
    "net/url"
    "path"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awscfg "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"npm-mirror/config"
)

// Client S3客户端
type Client struct {
	client        *s3.Client
	presignClient *s3.PresignClient
	bucket        string
	prefix        string
	config        *config.Config
}

// New 创建新的S3客户端
func New(cfg *config.Config) (*Client, error) {
    if cfg.S3Endpoint == "" {
        return nil, fmt.Errorf("S3 endpoint is required")
    }

	// 解析endpoint，确定服务类型
	endpointURL, err := url.Parse(cfg.S3Endpoint)
	if err != nil {
		return nil, fmt.Errorf("invalid S3 endpoint: %v", err)
	}

	// 创建AWS配置
    // 规范 region，避免 SDK 报错
    region := cfg.S3Region
    if region == "" || strings.EqualFold(region, "default") {
        region = "us-east-1"
    }

    awsCfg, err := awscfg.LoadDefaultConfig(context.TODO(),
        awscfg.WithRegion(region),
        awscfg.WithEndpointResolverWithOptions(aws.EndpointResolverWithOptionsFunc(
            func(service, region string, options ...interface{}) (aws.Endpoint, error) {
                return aws.Endpoint{
                    URL:               cfg.S3Endpoint,
                    HostnameImmutable: true,
                    SigningRegion:     region,
                }, nil
            })),
    )
    if err != nil {
        return nil, fmt.Errorf("failed to load AWS config: %v", err)
    }

	// 设置凭证
	awsCfg.Credentials = credentials.NewStaticCredentialsProvider(
		cfg.S3AccessKey,
		cfg.S3SecretKey,
		"",
	)

	// 创建S3客户端
    s3Client := s3.NewFromConfig(awsCfg, func(o *s3.Options) {
        // 对于阿里云OSS，需要设置此项
        if strings.Contains(endpointURL.Host, "aliyuncs.com") {
            o.UsePathStyle = true
        }
        // 对于腾讯云COS，需要设置此项
        if strings.Contains(endpointURL.Host, "myqcloud.com") {
            o.UsePathStyle = true
        }
        // 其他自定义兼容存储，统一走 PathStyle
        if !strings.Contains(endpointURL.Host, "amazonaws.com") {
            o.UsePathStyle = true
        }
    })

    return &Client{
        client:        s3Client,
        presignClient: s3.NewPresignClient(s3Client),
        bucket:        cfg.S3Bucket,
        prefix:        strings.ReplaceAll(cfg.S3Prefix, "\\", "/"),
        config:        cfg,
    }, nil
}

// UploadFile 上传文件到S3
func (c *Client) UploadFile(ctx context.Context, key string, reader io.Reader, contentType string, contentLength int64) error {
    // 确保key以prefix开头
    if !strings.HasPrefix(key, c.prefix) {
        key = path.Join(c.prefix, key)
    }
    key = strings.ReplaceAll(key, "\\", "/")

    in := &s3.PutObjectInput{
        Bucket:      aws.String(c.bucket),
        Key:         aws.String(key),
        Body:        reader,
        ContentType: aws.String(contentType),
    }
    if contentLength > 0 {
        in.ContentLength = aws.Int64(contentLength)
    }

    _, err := c.client.PutObject(ctx, in)

	return err
}

// DownloadFile 从S3下载文件
func (c *Client) DownloadFile(ctx context.Context, key string) (io.ReadCloser, error) {
    // 确保key以prefix开头
    if !strings.HasPrefix(key, c.prefix) {
        key = path.Join(c.prefix, key)
    }
    key = strings.ReplaceAll(key, "\\", "/")

	resp, err := c.client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(c.bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		return nil, err
	}

	return resp.Body, nil
}

// GetFileInfo 获取文件信息
func (c *Client) GetFileInfo(ctx context.Context, key string) (*types.Object, error) {
    // 确保key以prefix开头
    if !strings.HasPrefix(key, c.prefix) {
        key = path.Join(c.prefix, key)
    }
    key = strings.ReplaceAll(key, "\\", "/")

	resp, err := c.client.HeadObject(ctx, &s3.HeadObjectInput{
		Bucket: aws.String(c.bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		return nil, err
	}

	return &types.Object{
		Key:          aws.String(key),
		Size:         resp.ContentLength,
		LastModified: resp.LastModified,
		ETag:         resp.ETag,
	}, nil
}

// ListObjects 列出S3中的对象
func (c *Client) ListObjects(ctx context.Context, prefix string, delimiter string) ([]types.Object, error) {
    // 确保prefix以c.prefix开头
    fullPrefix := c.prefix
    if prefix != "" {
        fullPrefix = path.Join(c.prefix, prefix)
    }
    fullPrefix = strings.ReplaceAll(fullPrefix, "\\", "/")

	var objects []types.Object
	var continuationToken *string

	for {
		resp, err := c.client.ListObjectsV2(ctx, &s3.ListObjectsV2Input{
			Bucket:            aws.String(c.bucket),
			Prefix:            aws.String(fullPrefix),
			Delimiter:         aws.String(delimiter),
			ContinuationToken: continuationToken,
		})
		if err != nil {
			return nil, err
		}

		objects = append(objects, resp.Contents...)

    if !aws.ToBool(resp.IsTruncated) {
        break
    }

		continuationToken = resp.NextContinuationToken
	}

	return objects, nil
}

// DeleteObject 删除S3中的对象
func (c *Client) DeleteObject(ctx context.Context, key string) error {
    // 确保key以prefix开头
    if !strings.HasPrefix(key, c.prefix) {
        key = path.Join(c.prefix, key)
    }
    key = strings.ReplaceAll(key, "\\", "/")

	_, err := c.client.DeleteObject(ctx, &s3.DeleteObjectInput{
		Bucket: aws.String(c.bucket),
		Key:    aws.String(key),
	})

	return err
}

// GetPresignedURL 生成预签名下载链接
func (c *Client) GetPresignedURL(ctx context.Context, key string, lifetime time.Duration) (string, error) {
	// 确保key以prefix开头
	if !strings.HasPrefix(key, c.prefix) {
		key = path.Join(c.prefix, key)
	}
	key = strings.ReplaceAll(key, "\\", "/")

	presignedReq, err := c.presignClient.PresignGetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(c.bucket),
		Key:    aws.String(key),
	}, s3.WithPresignExpires(lifetime))

	if err != nil {
		return "", err
	}
	return presignedReq.URL, nil
}

// GetPackageKey 获取包在S3中的key
func (c *Client) GetPackageKey(packageName, version string) string {
    return strings.ReplaceAll(path.Join(c.prefix, packageName, version, fmt.Sprintf("%s-%s.tgz", packageName, version)), "\\", "/")
}

// GetPackageMetaKey 获取包元数据在S3中的key
func (c *Client) GetPackageMetaKey(packageName string) string {
    return strings.ReplaceAll(path.Join(c.prefix, packageName, "metadata.json"), "\\", "/")
}

func (c *Client) GetSyncStateKey() string {
    return strings.ReplaceAll(path.Join(c.prefix, "_index", "sync_state.json"), "\\", "/")
}
