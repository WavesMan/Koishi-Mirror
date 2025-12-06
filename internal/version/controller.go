package version

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"sort"
	"strings"

	"golang.org/x/sync/singleflight"
	"npm-mirror/config"
	cache "npm-mirror/internal/cache"
	"npm-mirror/internal/models"
	"npm-mirror/internal/s3client"
	pgstore "npm-mirror/internal/storage/postgres"
	"npm-mirror/internal/sync"
)

type Controller struct {
	cfg    *config.Config
	s3     *s3client.Client
	sm     *sync.SyncManager
	store  *pgstore.Store
	cache  *cache.Redis
	lru    *cache.LRU
	flight singleflight.Group
}

func NewController(cfg *config.Config, s3 *s3client.Client, sm *sync.SyncManager, store *pgstore.Store, c *cache.Redis, l *cache.LRU) *Controller {
	return &Controller{cfg: cfg, s3: s3, sm: sm, store: store, cache: c, lru: l}
}

type PackageMetadata struct {
	Name     string
	Versions []models.Package
	DistTags map[string]string
}

func (vc *Controller) BuildPackageMetadata(ctx context.Context, name string) (*PackageMetadata, error) {
	if vc.lru != nil {
		if s, ok := vc.lru.Get("pkg:meta:" + name); ok && s != "" {
			var md PackageMetadata
			if json.Unmarshal([]byte(s), &md) == nil && md.Name != "" {
				if vc.cache != nil && vc.cfg.CacheSoftTTL > 0 {
					if ttl, err := vc.cache.TTL(ctx, "pkg:meta:"+name); err == nil && ttl > 0 && ttl <= vc.cfg.CacheSoftTTL {
						go func() {
							_, _, _ = vc.flight.Do("pkg_meta:"+name, func() (interface{}, error) {
								return vc.rebuildAndCache(ctx, name)
							})
						}()
					}
				}
				return &md, nil
			}
		}
	}
	if vc.cache != nil {
		if s, err := vc.cache.Get(ctx, "pkg:meta:"+name); err == nil && s != "" {
			var md PackageMetadata
			if json.Unmarshal([]byte(s), &md) == nil && md.Name != "" {
				if vc.cfg.CacheSoftTTL > 0 {
					if ttl, err := vc.cache.TTL(ctx, "pkg:meta:"+name); err == nil && ttl > 0 && ttl <= vc.cfg.CacheSoftTTL {
						go func() {
							_, _, _ = vc.flight.Do("pkg_meta:"+name, func() (interface{}, error) {
								return vc.rebuildAndCache(ctx, name)
							})
						}()
					}
				}
				return &md, nil
			}
		}
	}
	v, errDo, _ := vc.flight.Do("pkg_meta:"+name, func() (interface{}, error) {
		return vc.rebuildAndCache(ctx, name)
	})
	if errDo != nil {
		return nil, errDo
	}
	if v == nil {
		return nil, errors.New("构建失败")
	}
	md, _ := v.(*PackageMetadata)
	return md, nil
}

func (vc *Controller) rebuildAndCache(ctx context.Context, name string) (interface{}, error) {
	var list []models.Package
	if vc.store != nil {
		l, err := vc.store.GetPackageVersions(ctx, name)
		if err == nil {
			list = l
		}
	}
	if len(list) == 0 {
		resp, err := http.Get(vc.cfg.DataSourceURL)
		if err != nil {
			return nil, err
		}
		defer func(Body io.ReadCloser) {
			_ = Body.Close()
		}(resp.Body)

		var data models.DataSource
		if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
			return nil, err
		}

		for _, p := range data.Packages {
			if p.Name == name {
				list = append(list, p)
			}
		}
	}
	if len(list) == 0 {
		return nil, errors.New("包不存在")
	}

	st := vc.sm.GetSyncState()
	for i := range list {
		key := list[i].Name + "@" + list[i].Version
		if s, ok := st.PackageStates[key]; ok {
			list[i].SyncStatus = s.SyncStatus
			list[i].SyncTime = s.SyncTime
			if s.Size > 0 {
				list[i].Dist.Size = s.Size
			}
		} else {
			list[i].SyncStatus = "pending"
		}
	}

	sort.Slice(list, func(i, j int) bool {
		return compareVersion(list[i].Version, list[j].Version) > 0
	})

	tags := map[string]string{}
	if vc.store != nil {
		t, err := vc.store.GetDistTags(ctx, name)
		if err == nil {
			tags = t
		}
	}
	if len(list) > 0 && tags["latest"] == "" {
		tags["latest"] = list[0].Version
	}
	md := &PackageMetadata{Name: name, Versions: list, DistTags: tags}
	if vc.cache != nil {
		if b, e := json.Marshal(md); e == nil {
			_ = vc.cache.Set(ctx, "pkg:meta:"+name, string(b))
		}
	}
	if vc.lru != nil {
		if b, e := json.Marshal(md); e == nil {
			vc.lru.Set("pkg:meta:"+name, string(b))
		}
	}
	return md, nil
}

func (vc *Controller) ResolveVersion(ctx context.Context, name string, rng string) (string, error) {
	md, err := vc.BuildPackageMetadata(ctx, name)
	if err != nil {
		return "", err
	}
	if rng == "" || strings.ToLower(rng) == "latest" {
		return md.DistTags["latest"], nil
	}
	for _, v := range md.Versions {
		if v.Version == rng {
			return v.Version, nil
		}
	}
	return "", errors.New("不支持的版本范围或版本不存在")
}

func compareVersion(a, b string) int {
	pa := strings.SplitN(a, "-", 2)[0]
	pb := strings.SplitN(b, "-", 2)[0]
	as := strings.Split(pa, ".")
	bs := strings.Split(pb, ".")
	for i := 0; i < 3; i++ {
		ai := 0
		bi := 0
		if i < len(as) {
			ai = atoiSafe(as[i])
		}
		if i < len(bs) {
			bi = atoiSafe(bs[i])
		}
		if ai != bi {
			if ai > bi {
				return 1
			} else {
				return -1
			}
		}
	}
	if a == b {
		return 0
	}
	if strings.Contains(a, "-") && !strings.Contains(b, "-") {
		return -1
	}
	if !strings.Contains(a, "-") && strings.Contains(b, "-") {
		return 1
	}
	if a > b {
		return 1
	} else {
		return -1
	}
}

func atoiSafe(s string) int {
	n := 0
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c < '0' || c > '9' {
			break
		}
		n = n*10 + int(c-'0')
	}
	return n
}
