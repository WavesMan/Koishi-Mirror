package version

import (
    "context"
    "errors"
    "net/http"
    "sort"
    "strings"
    "encoding/json"

    "npm-mirror/config"
    "npm-mirror/internal/models"
    "npm-mirror/internal/s3client"
    "npm-mirror/internal/sync"
    pgstore "npm-mirror/internal/storage/postgres"
)

type Controller struct {
    cfg *config.Config
    s3  *s3client.Client
    sm  *sync.SyncManager
    store *pgstore.Store
}

func NewController(cfg *config.Config, s3 *s3client.Client, sm *sync.SyncManager, store *pgstore.Store) *Controller {
    return &Controller{cfg: cfg, s3: s3, sm: sm, store: store}
}

type PackageMetadata struct {
    Name     string
    Versions []models.Package
    DistTags map[string]string
}

func (vc *Controller) BuildPackageMetadata(ctx context.Context, name string) (*PackageMetadata, error) {
    var list []models.Package
    if vc.store != nil {
        l, err := vc.store.GetPackageVersions(ctx, name)
        if err == nil { list = l }
    }
    if len(list) == 0 {
        resp, err := http.Get(vc.cfg.DataSourceURL)
        if err != nil {
            return nil, err
        }
        defer resp.Body.Close()

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
        if err == nil { tags = t }
    }
    if len(list) > 0 && tags["latest"] == "" {
        tags["latest"] = list[0].Version
    }

    return &PackageMetadata{Name: name, Versions: list, DistTags: tags}, nil
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
        if i < len(as) { ai = atoiSafe(as[i]) }
        if i < len(bs) { bi = atoiSafe(bs[i]) }
        if ai != bi {
            if ai > bi { return 1 } else { return -1 }
        }
    }
    if a == b { return 0 }
    if strings.Contains(a, "-") && !strings.Contains(b, "-") { return -1 }
    if !strings.Contains(a, "-") && strings.Contains(b, "-") { return 1 }
    if a > b { return 1 } else { return -1 }
}

func atoiSafe(s string) int {
    n := 0
    for i := 0; i < len(s); i++ {
        c := s[i]
        if c < '0' || c > '9' { break }
        n = n*10 + int(c-'0')
    }
    return n
}
