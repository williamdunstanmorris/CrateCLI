// Package cache is a tiny on-disk JSON cache. Discogs release data rarely
// changes, so caching it saves rate-limit budget on repeat digs.
package cache

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

type Cache struct {
	dir string
	ttl time.Duration
}

// New returns a cache rooted at the user cache dir. A nil *Cache is valid and
// caches nothing.
func New(namespace string, ttl time.Duration) *Cache {
	base, err := os.UserCacheDir()
	if err != nil {
		return nil
	}
	dir := filepath.Join(base, "cratecli", namespace)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil
	}
	return &Cache{dir: dir, ttl: ttl}
}

func (c *Cache) path(key string) string { return filepath.Join(c.dir, key+".json") }

// Get decodes the cached value for key into v. It reports whether it found a
// fresh entry.
func (c *Cache) Get(key string, v any) bool {
	if c == nil {
		return false
	}
	p := c.path(key)
	info, err := os.Stat(p)
	if err != nil || time.Since(info.ModTime()) > c.ttl {
		return false
	}
	b, err := os.ReadFile(p)
	if err != nil {
		return false
	}
	return json.Unmarshal(b, v) == nil
}

// Put stores v under key. Errors are ignored; the cache is best-effort.
func (c *Cache) Put(key string, v any) {
	if c == nil {
		return
	}
	b, err := json.Marshal(v)
	if err != nil {
		return
	}
	tmp := c.path(key) + ".tmp"
	if os.WriteFile(tmp, b, 0o600) == nil {
		_ = os.Rename(tmp, c.path(key))
	}
}
