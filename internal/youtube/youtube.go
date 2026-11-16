// Package youtube finds YouTube videos for a record.
//
// The YouTube Data API v3 gives each project 10,000 quota units per day by
// default, and a search.list call costs 100 units, so you get about
// 100 searches a day. cratecli therefore uses the API only as a fallback and
// always provides a plain search URL, which costs nothing.
package youtube

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
)

// ErrQuotaExceeded means the daily API quota is used up (resets at midnight
// Pacific time).
var ErrQuotaExceeded = errors.New("youtube: daily API quota exceeded")

type Video struct {
	URL     string `json:"url"`
	Title   string `json:"title"`
	Channel string `json:"channel,omitempty"`
}

// SearchURL returns a youtube.com results page for the query. It needs no
// API key and has no quota.
func SearchURL(query string) string {
	return "https://www.youtube.com/results?search_query=" + url.QueryEscape(query)
}

func WatchURL(id string) string { return "https://www.youtube.com/watch?v=" + id }

type Client struct {
	key  string
	http *http.Client
}

func New(apiKey string) *Client {
	return &Client{key: apiKey, http: &http.Client{Timeout: 15 * time.Second}}
}

func (c *Client) Enabled() bool { return c != nil && c.key != "" }

// Search returns up to max video results for query. It costs 100 quota units.
func (c *Client) Search(ctx context.Context, query string, max int) ([]Video, error) {
	if !c.Enabled() {
		return nil, errors.New("youtube: no API key configured")
	}
	v := url.Values{}
	v.Set("part", "snippet")
	v.Set("type", "video")
	v.Set("q", query)
	v.Set("maxResults", fmt.Sprint(max))
	v.Set("key", c.key)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		"https://www.googleapis.com/youtube/v3/search?"+v.Encode(), nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var out struct {
		Items []struct {
			ID struct {
				VideoID string `json:"videoId"`
			} `json:"id"`
			Snippet struct {
				Title        string `json:"title"`
				ChannelTitle string `json:"channelTitle"`
			} `json:"snippet"`
		} `json:"items"`
		Error *struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
			Errors  []struct {
				Reason string `json:"reason"`
			} `json:"errors"`
		} `json:"error"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, fmt.Errorf("youtube: decode: %w", err)
	}
	if out.Error != nil {
		for _, e := range out.Error.Errors {
			if e.Reason == "quotaExceeded" || e.Reason == "dailyLimitExceeded" {
				return nil, ErrQuotaExceeded
			}
		}
		return nil, fmt.Errorf("youtube: %d %s", out.Error.Code, out.Error.Message)
	}

	vids := make([]Video, 0, len(out.Items))
	for _, it := range out.Items {
		if it.ID.VideoID == "" {
			continue
		}
		vids = append(vids, Video{
			URL:     WatchURL(it.ID.VideoID),
			Title:   it.Snippet.Title,
			Channel: it.Snippet.ChannelTitle,
		})
	}
	return vids, nil
}
