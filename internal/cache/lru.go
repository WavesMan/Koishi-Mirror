package cache

import (
    "sync"
    "time"
)

type lruNode struct {
    key   string
    value string
    exp   int64
    prev  *lruNode
    next  *lruNode
}

type LRU struct {
    mu    sync.Mutex
    cap   int
    head  *lruNode
    tail  *lruNode
    items map[string]*lruNode
    ttl   time.Duration
}

func NewLRU(size int, ttl time.Duration) *LRU {
    if size <= 0 { size = 1 }
    return &LRU{cap: size, items: make(map[string]*lruNode), ttl: ttl}
}

func (l *LRU) Get(key string) (string, bool) {
    l.mu.Lock()
    n := l.items[key]
    if n == nil { l.mu.Unlock(); return "", false }
    if n.exp > 0 && time.Now().Unix() > n.exp { l.removeNode(n); delete(l.items, key); l.mu.Unlock(); return "", false }
    l.moveToFront(n)
    v := n.value
    l.mu.Unlock()
    return v, true
}

func (l *LRU) Set(key, val string) {
    l.mu.Lock()
    if n := l.items[key]; n != nil {
        n.value = val
        if l.ttl > 0 { n.exp = time.Now().Add(l.ttl).Unix() }
        l.moveToFront(n)
        l.mu.Unlock()
        return
    }
    n := &lruNode{key: key, value: val}
    if l.ttl > 0 { n.exp = time.Now().Add(l.ttl).Unix() }
    l.items[key] = n
    l.addToFront(n)
    if len(l.items) > l.cap { l.evictTail() }
    l.mu.Unlock()
}

func (l *LRU) Del(key string) {
    l.mu.Lock()
    if n := l.items[key]; n != nil {
        l.removeNode(n)
        delete(l.items, key)
    }
    l.mu.Unlock()
}

func (l *LRU) addToFront(n *lruNode) {
    n.prev = nil
    n.next = l.head
    if l.head != nil { l.head.prev = n }
    l.head = n
    if l.tail == nil { l.tail = n }
}

func (l *LRU) moveToFront(n *lruNode) {
    if l.head == n { return }
    l.removeNode(n)
    l.addToFront(n)
}

func (l *LRU) removeNode(n *lruNode) {
    if n.prev != nil { n.prev.next = n.next } else { l.head = n.next }
    if n.next != nil { n.next.prev = n.prev } else { l.tail = n.prev }
    n.prev = nil
    n.next = nil
}

func (l *LRU) evictTail() {
    if l.tail == nil { return }
    k := l.tail.key
    l.removeNode(l.tail)
    delete(l.items, k)
}
