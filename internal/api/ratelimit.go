package api

import (
    "net"
    "sync"
    "time"

    "github.com/gin-gonic/gin"
    "npm-mirror/config"
)

type bucket struct {
    mu     sync.Mutex
    tokens float64
    last   time.Time
    rate   float64
    burst  float64
}

func newBucket(qps int) *bucket {
    if qps <= 0 { return nil }
    return &bucket{tokens: float64(qps), last: time.Now(), rate: float64(qps), burst: float64(qps)}
}

func (b *bucket) allow() bool {
    if b == nil { return true }
    b.mu.Lock()
    now := time.Now()
    dt := now.Sub(b.last).Seconds()
    b.tokens += dt * b.rate
    if b.tokens > b.burst { b.tokens = b.burst }
    b.last = now
    if b.tokens >= 1 {
        b.tokens -= 1
        b.mu.Unlock()
        return true
    }
    b.mu.Unlock()
    return false
}

func clientIP(c *gin.Context) string {
    ip := c.ClientIP()
    if ip == "" {
        host, _, err := net.SplitHostPort(c.Request.RemoteAddr)
        if err == nil { ip = host }
    }
    return ip
}

func RateLimitMiddleware(cfg *config.Config) gin.HandlerFunc {
    global := newBucket(cfg.RLGlobalQPS)
    var mu sync.Mutex
    perIP := map[string]*bucket{}
    lastSeen := map[string]time.Time{}
    ttl := 2 * time.Minute
    return func(c *gin.Context) {
        if global != nil && !global.allow() {
            c.Header("Retry-After", "1")
            c.AbortWithStatus(429)
            return
        }
        if cfg.RLPerIPQPS > 0 {
            ip := clientIP(c)
            mu.Lock()
            b := perIP[ip]
            if b == nil { b = newBucket(cfg.RLPerIPQPS); perIP[ip] = b }
            lastSeen[ip] = time.Now()
            for k, t := range lastSeen {
                if time.Since(t) > ttl { delete(perIP, k); delete(lastSeen, k) }
            }
            mu.Unlock()
            if b != nil && !b.allow() {
                c.Header("Retry-After", "1")
                c.AbortWithStatus(429)
                return
            }
        }
        c.Next()
    }
}

