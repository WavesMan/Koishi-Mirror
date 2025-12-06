package datasource

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"

	"npm-mirror/config"
	"npm-mirror/internal/cache"
	"npm-mirror/internal/logging"
	"npm-mirror/internal/models"
)

type Source struct {
	cfg    *config.Config
	client *http.Client
	cache  *cache.Redis
	logger *logging.Logger
}

func New(cfg *config.Config, cache *cache.Redis) *Source {
	tr := &http.Transport{
		MaxIdleConns:          100,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
		MaxConnsPerHost:       100,
	}
	return &Source{
		cfg:    cfg,
		client: &http.Client{Transport: tr, Timeout: cfg.DataSourceTimeout},
		cache:  cache,
	}
}

func (s *Source) SetLogger(l *logging.Logger) { s.logger = l }

func (s *Source) ClearCache(ctx context.Context) {
	if s.cache != nil {
		_ = s.cache.Del(ctx, "data_source:index")
	}
	if s.logger != nil {
		s.logger.Info("cache", "data_source_cleared", nil)
	}
}

func (s *Source) Get(ctx context.Context) (models.DataSource, error) {
	var ds models.DataSource
	// Try cache first
	if s.cache != nil {
		if raw, err := s.cache.Get(ctx, "data_source:index"); err == nil && raw != "" {
			_ = json.Unmarshal([]byte(raw), &ds)
			if s.logger != nil {
				s.logger.Debug("datasource", "cache_hit", map[string]interface{}{"packages": len(ds.Packages), "total": ds.Total})
			}
			if len(ds.Packages) > 0 {
				return ds, nil
			}
		}
	}

	// Retry fetch
	var lastErr error
	for i := 0; i < s.cfg.DataSourceMaxRetries; i++ {
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, s.cfg.DataSourceURL, nil)
		req.Header.Set("Accept", "application/json")
		if s.logger != nil {
			s.logger.Debug("datasource", "fetch_start", map[string]interface{}{"url": s.cfg.DataSourceURL})
		}
		resp, err := s.client.Do(req)
		if err != nil {
			lastErr = err
			if s.logger != nil {
				s.logger.Error("datasource", "fetch_error", map[string]interface{}{"error": err.Error()})
			}
		} else {
			b, _ := io.ReadAll(resp.Body)
			_ = resp.Body.Close()
			if s.logger != nil {
				s.logger.Debug("datasource", "fetch_done", map[string]interface{}{"status": resp.StatusCode, "bytes": len(b)})
			}
			// first try strict decode
			if err := json.Unmarshal(b, &ds); err == nil && (len(ds.Packages) > 0) {
				if ds.Total == 0 {
					ds.Total = len(ds.Packages)
				}
				if bb, err := json.Marshal(ds); err == nil && s.cache != nil {
					_ = s.cache.Set(ctx, "data_source:index", string(bb))
				}
				if s.logger != nil {
					s.logger.Debug("datasource", "parse_strict", map[string]interface{}{"packages": len(ds.Packages)})
				}
				return ds, nil
			}
			// fallback normalize
			nds := normalizeDataSource(b)
			if len(nds.Packages) > 0 {
				if nds.Total == 0 {
					nds.Total = len(nds.Packages)
				}
				if bb, err := json.Marshal(nds); err == nil && s.cache != nil {
					_ = s.cache.Set(ctx, "data_source:index", string(bb))
				}
				if s.logger != nil {
					s.logger.Debug("datasource", "parse_normalized", map[string]interface{}{"packages": len(nds.Packages)})
				}
				return nds, nil
			}
			lastErr = err
			if s.logger != nil {
				s.logger.Warn("datasource", "parse_empty", map[string]interface{}{"total": ds.Total})
			}
		}
		time.Sleep(time.Duration(i+1) * 500 * time.Millisecond)
	}
	// Last try: return cached even if expired
	if s.cache != nil {
		if raw, err := s.cache.Get(ctx, "data_source:index"); err == nil && raw != "" {
			_ = json.Unmarshal([]byte(raw), &ds)
			if s.logger != nil {
				s.logger.Debug("datasource", "cache_fallback", map[string]interface{}{"packages": len(ds.Packages), "total": ds.Total})
			}
			return ds, nil
		}
	}
	return models.DataSource{}, lastErr
}

// normalizeDataSource 尝试从多种结构中提取包列表
func normalizeDataSource(b []byte) models.DataSource {
	var ds models.DataSource
	type Pkg struct {
		Name    string `json:"name"`
		Version string `json:"version"`
		Dist    struct {
			Tarball string `json:"tarball"`
			Size    int64  `json:"size"`
			Shasum  string `json:"shasum"`
		} `json:"dist"`
	}
	var c1 struct {
		Total    int   `json:"total"`
		Packages []Pkg `json:"packages"`
	}
	if json.Unmarshal(b, &c1) == nil && len(c1.Packages) > 0 {
		ds.Total = c1.Total
		for _, p := range c1.Packages {
			var m models.Package
			m.Name = p.Name
			m.Version = p.Version
			m.Dist.Tarball = p.Dist.Tarball
			m.Dist.Size = p.Dist.Size
			m.Dist.Shasum = p.Dist.Shasum
			ds.Packages = append(ds.Packages, m)
		}
		return ds
	}
	var c2 struct {
		Items []struct {
			Name    string `json:"name"`
			Version string `json:"version"`
			Tarball string `json:"tarball"`
		} `json:"items"`
		Total int `json:"total"`
	}
	if json.Unmarshal(b, &c2) == nil && len(c2.Items) > 0 {
		ds.Total = c2.Total
		for _, it := range c2.Items {
			var m models.Package
			m.Name = it.Name
			m.Version = it.Version
			m.Dist.Tarball = it.Tarball
			ds.Packages = append(ds.Packages, m)
		}
		return ds
	}
	var c3 struct {
		Rows []struct {
			N string `json:"n"`
			V string `json:"v"`
		} `json:"rows"`
		Total int `json:"total"`
	}
	if json.Unmarshal(b, &c3) == nil && len(c3.Rows) > 0 {
		ds.Total = c3.Total
		for _, r := range c3.Rows {
			var m models.Package
			m.Name = r.N
			m.Version = r.V
			ds.Packages = append(ds.Packages, m)
		}
		return ds
	}
	var arr []Pkg
	if json.Unmarshal(b, &arr) == nil && len(arr) > 0 {
		for _, p := range arr {
			var m models.Package
			m.Name = p.Name
			m.Version = p.Version
			m.Dist.Tarball = p.Dist.Tarball
			ds.Packages = append(ds.Packages, m)
		}
		ds.Total = len(ds.Packages)
		return ds
	}
	var c5 struct {
		Total   int `json:"total"`
		Objects []struct {
			Package struct {
				Name        string `json:"name"`
				Version     string `json:"version"`
				Description string `json:"description"`
				Publisher   struct {
					Name string `json:"name"`
				} `json:"publisher"`
			} `json:"package"`
		} `json:"objects"`
	}
	if json.Unmarshal(b, &c5) == nil && len(c5.Objects) > 0 {
		ds.Total = c5.Total
		for _, it := range c5.Objects {
			var m models.Package
			m.Name = it.Package.Name
			m.Version = it.Package.Version
			m.Description = it.Package.Description
			m.Author = it.Package.Publisher.Name
			m.Dist.Tarball = buildNpmTarball(m.Name, m.Version)
			ds.Packages = append(ds.Packages, m)
		}
		if ds.Total == 0 {
			ds.Total = len(ds.Packages)
		}
		return ds
	}
	return models.DataSource{}
}

func buildNpmTarball(name, version string) string {
	file := name
	if strings.HasPrefix(file, "@") {
		if idx := strings.Index(file, "/"); idx != -1 {
			file = file[idx+1:]
		}
	}
	return "https://registry.npmjs.org/" + name + "/-/" + file + "-" + version + ".tgz"
}
