package sources

import (
	"bytes"
	"context"
	"crypto/sha1"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Fetcher is a polite HTTP client for scraping: honest User-Agent, a minimum
// gap between requests to the same host, and a short on-disk page cache so
// repeat runs within the hour don't hit the site again.
type Fetcher struct {
	http      *http.Client
	userAgent string
	cacheDir  string // "" disables caching
	ttl       time.Duration
	gap       time.Duration

	mu   sync.Mutex
	last map[string]time.Time
}

// NewFetcher creates a Fetcher. noCache disables the page cache.
func NewFetcher(userAgent string, noCache bool) *Fetcher {
	f := &Fetcher{
		http:      &http.Client{Timeout: 25 * time.Second},
		userAgent: userAgent,
		ttl:       time.Hour,
		gap:       1500 * time.Millisecond,
		last:      map[string]time.Time{},
	}
	if !noCache {
		if base, err := os.UserCacheDir(); err == nil {
			dir := filepath.Join(base, "cratecli", "pages")
			if os.MkdirAll(dir, 0o700) == nil {
				f.cacheDir = dir
			}
		}
	}
	return f
}

// Get fetches url (GET).
func (f *Fetcher) Get(ctx context.Context, url string) ([]byte, error) {
	return f.do(ctx, http.MethodGet, url, nil, "")
}

// PostJSON posts a JSON body.
func (f *Fetcher) PostJSON(ctx context.Context, url string, body []byte) ([]byte, error) {
	return f.do(ctx, http.MethodPost, url, body, "application/json")
}

func (f *Fetcher) do(ctx context.Context, method, url string, body []byte, contentType string) ([]byte, error) {
	key := fmt.Sprintf("%x", sha1.Sum(append([]byte(method+" "+url+"\n"), body...)))
	if b, ok := f.cached(key); ok {
		return b, nil
	}

	// Retry dropped connections (not HTTP errors) a couple of times.
	var resp *http.Response
	var req *http.Request
	var b []byte
	for attempt := 0; ; attempt++ {
		var err error
		req, err = http.NewRequestWithContext(ctx, method, url, bytes.NewReader(body))
		if err != nil {
			return nil, err
		}
		req.Header.Set("User-Agent", f.userAgent)
		req.Header.Set("Accept-Language", "en")
		if contentType != "" {
			req.Header.Set("Content-Type", contentType)
		}
		if err := f.wait(ctx, req.URL.Host); err != nil {
			return nil, err
		}
		resp, err = f.http.Do(req)
		if err == nil {
			b, err = io.ReadAll(io.LimitReader(resp.Body, 20<<20))
			resp.Body.Close()
			if err == nil {
				break
			}
		}
		if attempt >= 2 || ctx.Err() != nil {
			return nil, err
		}
	}
	if resp.StatusCode != http.StatusOK {
		if resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusTooManyRequests {
			return nil, fmt.Errorf("%s refused the request (%d); the site may block automated access", req.URL.Host, resp.StatusCode)
		}
		return nil, fmt.Errorf("%s: HTTP %d", url, resp.StatusCode)
	}
	f.store(key, b)
	return b, nil
}

func (f *Fetcher) wait(ctx context.Context, host string) error {
	f.mu.Lock()
	next := f.last[host].Add(f.gap)
	now := time.Now()
	if next.Before(now) {
		next = now
	}
	f.last[host] = next
	f.mu.Unlock()
	select {
	case <-time.After(time.Until(next)):
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (f *Fetcher) cached(key string) ([]byte, bool) {
	if f.cacheDir == "" {
		return nil, false
	}
	p := filepath.Join(f.cacheDir, key)
	info, err := os.Stat(p)
	if err != nil || time.Since(info.ModTime()) > f.ttl {
		return nil, false
	}
	b, err := os.ReadFile(p)
	return b, err == nil
}

func (f *Fetcher) store(key string, b []byte) {
	if f.cacheDir == "" {
		return
	}
	p := filepath.Join(f.cacheDir, key)
	if os.WriteFile(p+".tmp", b, 0o600) == nil {
		_ = os.Rename(p+".tmp", p)
	}
}
