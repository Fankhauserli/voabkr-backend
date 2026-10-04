package cache

import (
	"strings"
	"sync"
	"time"
)

type memoryEntry struct {
	data      []byte
	expiresAt time.Time
}

// MemoryCache provides a thread-safe in-memory cache with TTL support and background cleanup.
type MemoryCache struct {
	mu      sync.RWMutex
	entries map[string]memoryEntry
	stopCh  chan struct{}
}

// NewMemoryCache creates a new MemoryCache and starts a background cleanup worker if cleanupInterval > 0.
func NewMemoryCache(cleanupInterval time.Duration) *MemoryCache {
	mc := &MemoryCache{
		entries: make(map[string]memoryEntry),
		stopCh:  make(chan struct{}),
	}
	if cleanupInterval > 0 {
		go mc.startCleanup(cleanupInterval)
	}
	return mc
}

// Get retrieves a key from local memory if it exists and has not expired.
func (m *MemoryCache) Get(key string) ([]byte, bool) {
	m.mu.RLock()
	entry, ok := m.entries[key]
	m.mu.RUnlock()
	if !ok {
		return nil, false
	}
	if !entry.expiresAt.IsZero() && time.Now().After(entry.expiresAt) {
		m.mu.Lock()
		delete(m.entries, key)
		m.mu.Unlock()
		return nil, false
	}
	return entry.data, true
}

// Set stores a key and byte payload with an expiration TTL.
func (m *MemoryCache) Set(key string, data []byte, ttl time.Duration) {
	var expiresAt time.Time
	if ttl > 0 {
		expiresAt = time.Now().Add(ttl)
	}
	m.mu.Lock()
	m.entries[key] = memoryEntry{
		data:      data,
		expiresAt: expiresAt,
	}
	m.mu.Unlock()
}

// Delete removes one or more keys from local memory.
func (m *MemoryCache) Delete(keys ...string) {
	m.mu.Lock()
	for _, k := range keys {
		delete(m.entries, k)
	}
	m.mu.Unlock()
}

// DeletePrefix removes all keys matching the specified prefix from local memory.
func (m *MemoryCache) DeletePrefix(prefix string) {
	m.mu.Lock()
	for k := range m.entries {
		if strings.HasPrefix(k, prefix) {
			delete(m.entries, k)
		}
	}
	m.mu.Unlock()
}

// Clear flushes all entries in local memory.
func (m *MemoryCache) Clear() {
	m.mu.Lock()
	m.entries = make(map[string]memoryEntry)
	m.mu.Unlock()
}

// Close stops the background expiration cleanup worker.
func (m *MemoryCache) Close() {
	select {
	case <-m.stopCh:
	default:
		close(m.stopCh)
	}
}

func (m *MemoryCache) startCleanup(interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			now := time.Now()
			m.mu.Lock()
			for k, entry := range m.entries {
				if !entry.expiresAt.IsZero() && now.After(entry.expiresAt) {
					delete(m.entries, k)
				}
			}
			m.mu.Unlock()
		case <-m.stopCh:
			return
		}
	}
}
