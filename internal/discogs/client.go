// Package discogs is a small, rate-limit-aware client for the parts of the
// Discogs API that cratecli uses.
//
// Limits (as of late 2026):
//   - 60 requests/minute with a personal access token, 25/minute without.
//   - Database search returns at most 10,000 results (pagination is capped).
//   - Other users' marketplace inventories cannot be paged past page 100
//     (10,000 listings at per_page=100) for any given sort order.
package discogs

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"golang.org/x/time/rate"
)

const baseURL = "https://api.discogs.com"

type Client struct {
	http      *http.Client
	token     string
	userAgent string
	limiter   *rate.Limiter
}

// New creates a client. With an empty token, requests are unauthenticated
// and throttled to the lower anonymous limit.
func New(token, userAgent string) *Client {
	perMinute := 25.0
	if token != "" {
		perMinute = 60.0
	}
	// Leave a little headroom below the documented ceiling.
	r := rate.Limit(perMinute * 0.9 / 60.0)
	return &Client{
		http:      &http.Client{Timeout: 30 * time.Second},
		token:     token,
		userAgent: userAgent,
		limiter:   rate.NewLimiter(r, 5),
	}
}

// Authenticated reports whether a token is configured.
func (c *Client) Authenticated() bool { return c.token != "" }

// APIError is a non-2xx response from Discogs.
type APIError struct {
	Status  int
	Message string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("discogs: %d %s", e.Status, e.Message)
}

func (c *Client) get(ctx context.Context, path string, q url.Values, out any) error {
	u := baseURL + path
	if len(q) > 0 {
		u += "?" + q.Encode()
	}

	const maxAttempts = 4
	for attempt := 1; ; attempt++ {
		if err := c.limiter.Wait(ctx); err != nil {
			return err
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
		if err != nil {
			return err
		}
		req.Header.Set("User-Agent", c.userAgent)
		req.Header.Set("Accept", "application/vnd.discogs.v2.discogs+json")
		if c.token != "" {
			req.Header.Set("Authorization", "Discogs token="+c.token)
		}

		resp, err := c.http.Do(req)
		if err != nil {
			return err
		}
		body, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			return err
		}

		if resp.StatusCode == http.StatusTooManyRequests && attempt < maxAttempts {
			// Discogs uses a moving 60s window; wait it out progressively.
			wait := time.Duration(attempt*10) * time.Second
			if s, err := strconv.Atoi(resp.Header.Get("Retry-After")); err == nil && s > 0 {
				wait = time.Duration(s) * time.Second
			}
			select {
			case <-time.After(wait):
				continue
			case <-ctx.Done():
				return ctx.Err()
			}
		}

		if resp.StatusCode < 200 || resp.StatusCode > 299 {
			var m struct {
				Message string `json:"message"`
			}
			_ = json.Unmarshal(body, &m)
			if m.Message == "" {
				m.Message = strings.TrimSpace(string(body))
			}
			return &APIError{Status: resp.StatusCode, Message: m.Message}
		}
		return json.Unmarshal(body, out)
	}
}
