package proxy

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
)

// UpstreamProxy 上游代理服务
type UpstreamProxy struct {
	upstreamURL string
	client      *http.Client
}

// NewUpstreamProxy 创建新的上游代理
func NewUpstreamProxy(upstreamURL string) *UpstreamProxy {
	if upstreamURL == "" {
		upstreamURL = "https://registry.npmjs.org"
	}
	return &UpstreamProxy{
		upstreamURL: upstreamURL,
		client:      &http.Client{},
	}
}

// ProxyMetadata 代理包元数据请求
func (p *UpstreamProxy) ProxyMetadata(packageName string) (*http.Response, error) {
	targetURL := fmt.Sprintf("%s/%s", p.upstreamURL, url.PathEscape(packageName))
	return p.client.Get(targetURL)
}

// GetTarballURL 获取指定版本的 tarball URL
func (p *UpstreamProxy) GetTarballURL(packageName, version string) (string, error) {
	resp, err := p.ProxyMetadata(packageName)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("upstream returned status %d", resp.StatusCode)
	}

	var meta struct {
		Versions map[string]struct {
			Dist struct {
				Tarball string `json:"tarball"`
			} `json:"dist"`
		} `json:"versions"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&meta); err != nil {
		return "", err
	}

	versionInfo, ok := meta.Versions[version]
	if !ok || versionInfo.Dist.Tarball == "" {
		return "", fmt.Errorf("version %s not found in upstream", version)
	}

	return versionInfo.Dist.Tarball, nil
}

// ProxyTarball 代理 tarball 下载请求
func (p *UpstreamProxy) ProxyTarball(tarballURL string) (*http.Response, error) {
	return p.client.Get(tarballURL)
}

// StreamResponse 将上游响应流式传输到目标 writer
func StreamResponse(resp *http.Response, w http.ResponseWriter) error {
	// 复制响应头
	for k, v := range resp.Header {
		for _, vv := range v {
			w.Header().Add(k, vv)
		}
	}

	// 设置状态码
	w.WriteHeader(resp.StatusCode)

	// 流式复制响应体
	_, err := io.Copy(w, resp.Body)
	return err
}
