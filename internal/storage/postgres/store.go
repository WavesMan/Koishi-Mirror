package postgres

import (
    "context"
    "time"
    "encoding/json"
    "github.com/jackc/pgx/v5/pgxpool"
    "npm-mirror/internal/models"
)

type Store struct {
    pool *pgxpool.Pool
}

func New(ctx context.Context, dsn string) (*Store, error) {
    p, err := pgxpool.New(ctx, dsn)
    if err != nil { return nil, err }
    return &Store{pool: p}, nil
}

func (s *Store) Close() { if s.pool != nil { s.pool.Close() } }

func (s *Store) EnsureSchema(ctx context.Context) error {
    stmts := []string{
        `CREATE TABLE IF NOT EXISTS packages (
            name TEXT PRIMARY KEY,
            description TEXT,
            author TEXT
        )`,
        `CREATE TABLE IF NOT EXISTS package_versions (
            id BIGSERIAL PRIMARY KEY,
            name TEXT NOT NULL REFERENCES packages(name) ON DELETE CASCADE,
            version TEXT NOT NULL,
            tarball TEXT,
            size BIGINT,
            shasum TEXT,
            sync_status TEXT,
            sync_time TIMESTAMPTZ,
            UNIQUE(name, version)
        )`,
        `CREATE TABLE IF NOT EXISTS dist_tags (
            name TEXT NOT NULL REFERENCES packages(name) ON DELETE CASCADE,
            tag TEXT NOT NULL,
            version TEXT NOT NULL,
            PRIMARY KEY(name, tag)
        )`,
        `CREATE TABLE IF NOT EXISTS sync_state (
            package_key TEXT PRIMARY KEY,
            version TEXT,
            sync_status TEXT,
            sync_time TIMESTAMPTZ,
            retry_count INT,
            size BIGINT
        )`,
        `CREATE TABLE IF NOT EXISTS logs (
            id BIGSERIAL PRIMARY KEY,
            ts TIMESTAMPTZ NOT NULL DEFAULT NOW(),
            level TEXT,
            category TEXT,
            message TEXT,
            fields JSONB
        )`,
    }
    for _, q := range stmts {
        if _, err := s.pool.Exec(ctx, q); err != nil { return err }
    }
    return nil
}

func (s *Store) SaveSyncState(ctx context.Context, st *models.SyncState) error {
    for k, v := range st.PackageStates {
        _, err := s.pool.Exec(ctx, `INSERT INTO sync_state(package_key, version, sync_status, sync_time, retry_count, size)
VALUES($1,$2,$3,$4,$5,$6)
ON CONFLICT(package_key) DO UPDATE SET version=EXCLUDED.version, sync_status=EXCLUDED.sync_status, sync_time=EXCLUDED.sync_time, retry_count=EXCLUDED.retry_count, size=EXCLUDED.size`,
            k, v.Version, v.SyncStatus, v.SyncTime, v.RetryCount, v.Size)
        if err != nil { return err }
    }
    return nil
}

func (s *Store) LoadSyncState(ctx context.Context) (*models.SyncState, error) {
    rows, err := s.pool.Query(ctx, `SELECT package_key, version, sync_status, sync_time, retry_count, size FROM sync_state`)
    if err != nil { return nil, err }
    defer rows.Close()
    st := &models.SyncState{PackageStates: map[string]models.PackageState{}, LastSyncTime: time.Now()}
    for rows.Next() {
        var k, v, ss string
        var tm time.Time
        var rc int
        var sz int64
        if err := rows.Scan(&k, &v, &ss, &tm, &rc, &sz); err != nil { return nil, err }
        st.PackageStates[k] = models.PackageState{Version: v, SyncStatus: ss, SyncTime: tm, RetryCount: rc, Size: sz}
    }
    return st, nil
}

func (s *Store) UpsertPackageVersion(ctx context.Context, name, description, author string, pkg models.Package) error {
    if _, err := s.pool.Exec(ctx, `INSERT INTO packages(name, description, author) VALUES($1,$2,$3)
ON CONFLICT(name) DO UPDATE SET description=EXCLUDED.description, author=EXCLUDED.author`, name, description, author); err != nil { return err }
    _, err := s.pool.Exec(ctx, `INSERT INTO package_versions(name, version, tarball, size, shasum, sync_status, sync_time)
VALUES($1,$2,$3,$4,$5,$6,$7)
ON CONFLICT(name, version) DO UPDATE SET tarball=EXCLUDED.tarball, size=EXCLUDED.size, shasum=EXCLUDED.shasum, sync_status=EXCLUDED.sync_status, sync_time=EXCLUDED.sync_time`,
        name, pkg.Version, pkg.Dist.Tarball, pkg.Dist.Size, pkg.Dist.Shasum, pkg.SyncStatus, pkg.SyncTime)
    return err
}

func (s *Store) GetPackageVersions(ctx context.Context, name string) ([]models.Package, error) {
    rows, err := s.pool.Query(ctx, `SELECT pv.version, pv.tarball, pv.size, pv.shasum, pv.sync_status, pv.sync_time, p.description, p.author FROM package_versions pv JOIN packages p ON pv.name=p.name WHERE pv.name=$1 ORDER BY pv.sync_time DESC`, name)
    if err != nil { return nil, err }
    defer rows.Close()
    var list []models.Package
    for rows.Next() {
        var v models.Package
        v.Name = name
        var tarball string
        var size int64
        var shasum string
        var status string
        var tm time.Time
        var desc string
        var author string
        if err := rows.Scan(&v.Version, &tarball, &size, &shasum, &status, &tm, &desc, &author); err != nil { return nil, err }
        v.Dist.Tarball = tarball
        v.Dist.Size = size
        v.Dist.Shasum = shasum
        v.SyncStatus = status
        v.SyncTime = tm
        v.Description = desc
        v.Author = author
        list = append(list, v)
    }
    return list, nil
}

func (s *Store) HasVersion(ctx context.Context, name, version string) (bool, error) {
    var exists bool
    err := s.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM package_versions WHERE name=$1 AND version=$2)`, name, version).Scan(&exists)
    if err != nil { return false, err }
    return exists, nil
}

func (s *Store) UpdateVersionStatus(ctx context.Context, name, version, status string, size int64) error {
    _, err := s.pool.Exec(ctx, `UPDATE package_versions SET sync_status=$3, size=$4, sync_time=NOW() WHERE name=$1 AND version=$2`, name, version, status, size)
    return err
}

func (s *Store) CountDistinctPackages(ctx context.Context) (int, error) {
    var n int
    err := s.pool.QueryRow(ctx, `SELECT COUNT(*) FROM packages`).Scan(&n)
    return n, err
}

func (s *Store) CountVersions(ctx context.Context) (int, error) {
    var n int
    err := s.pool.QueryRow(ctx, `SELECT COUNT(*) FROM package_versions`).Scan(&n)
    return n, err
}

func (s *Store) StatusCounts(ctx context.Context) (map[string]int, error) {
    rows, err := s.pool.Query(ctx, `SELECT COALESCE(sync_status,'') as s, COUNT(*) FROM package_versions GROUP BY s`)
    if err != nil { return nil, err }
    defer rows.Close()
    m := map[string]int{}
    for rows.Next() {
        var s string
        var c int
        if err := rows.Scan(&s, &c); err != nil { return nil, err }
        m[s] = c
    }
    return m, nil
}

func (s *Store) LatestSyncTime(ctx context.Context) (time.Time, error) {
    var t time.Time
    err := s.pool.QueryRow(ctx, `SELECT COALESCE(MAX(sync_time), NOW()) FROM package_versions`).Scan(&t)
    return t, err
}

func (s *Store) InsertLog(ctx context.Context, level, category, message string, fields map[string]interface{}) error {
    var b []byte
    if fields != nil {
        bb, _ := json.Marshal(fields)
        b = bb
    }
    _, err := s.pool.Exec(ctx, `INSERT INTO logs(level, category, message, fields) VALUES($1,$2,$3,COALESCE($4::jsonb,'{}'::jsonb))`, level, category, message, string(b))
    return err
}

func (s *Store) GetDistTags(ctx context.Context, name string) (map[string]string, error) {
    rows, err := s.pool.Query(ctx, `SELECT tag, version FROM dist_tags WHERE name=$1`, name)
    if err != nil { return nil, err }
    defer rows.Close()
    tags := map[string]string{}
    for rows.Next() {
        var t, v string
        if err := rows.Scan(&t, &v); err != nil { return nil, err }
        tags[t] = v
    }
    return tags, nil
}

func (s *Store) SetDistTag(ctx context.Context, name, tag, version string) error {
    _, err := s.pool.Exec(ctx, `INSERT INTO dist_tags(name, tag, version) VALUES($1,$2,$3)
ON CONFLICT(name, tag) DO UPDATE SET version=EXCLUDED.version`, name, tag, version)
    return err
}
type PV struct {
    Name    string
    Version string
    Size    int64
}

func (s *Store) ListAllVersions(ctx context.Context) ([]PV, error) {
    rows, err := s.pool.Query(ctx, `SELECT name, version, size FROM package_versions`)
    if err != nil { return nil, err }
    defer rows.Close()
    var list []PV
    for rows.Next() {
        var r PV
        if err := rows.Scan(&r.Name, &r.Version, &r.Size); err != nil { return nil, err }
        list = append(list, r)
    }
    return list, nil
}

func (s *Store) PurgeLogsOlderThan(ctx context.Context, days int) error {
    _, err := s.pool.Exec(ctx, `DELETE FROM logs WHERE ts < NOW() - ($1::text || ' days')::interval`, days)
    return err
}
