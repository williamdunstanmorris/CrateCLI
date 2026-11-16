package youtube

// Account is your own YouTube account, reached with OAuth: reading your
// liked videos and liking new ones. (Client, in youtube.go, only needs an
// API key and can only search.)

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"

	"golang.org/x/oauth2"
)

const apiBase = "https://www.googleapis.com/youtube/v3"

// Account calls the YouTube API as you.
type Account struct {
	http *http.Client
	base string // overridden in tests
}

// NewAccount uses ts for access tokens (see Login and TokenFile).
func NewAccount(ctx context.Context, ts oauth2.TokenSource) *Account {
	return &Account{http: oauth2.NewClient(ctx, ts), base: apiBase}
}

// LikedVideo is one video from your likes.
type LikedVideo struct {
	ID         string `json:"id"`
	Title      string `json:"title"`
	Channel    string `json:"channel"`
	CategoryID string `json:"category_id"`
}

// URL is the video's watch page.
func (v LikedVideo) URL() string { return WatchURL(v.ID) }

// LikesPage is one page (up to 50) of liked videos, newest likes first.
type LikesPage struct {
	Videos []LikedVideo
	Next   string // token for the next page; "" at the end
	Total  int
}

// Likes fetches a page of your liked videos. It costs 1 quota unit.
func (a *Account) Likes(ctx context.Context, pageToken string) (LikesPage, error) {
	q := url.Values{"part": {"snippet"}, "myRating": {"like"}, "maxResults": {"50"}}
	if pageToken != "" {
		q.Set("pageToken", pageToken)
	}
	var resp struct {
		NextPageToken string `json:"nextPageToken"`
		PageInfo      struct {
			TotalResults int `json:"totalResults"`
		} `json:"pageInfo"`
		Items []struct {
			ID      string `json:"id"`
			Snippet struct {
				Title        string `json:"title"`
				ChannelTitle string `json:"channelTitle"`
				CategoryID   string `json:"categoryId"`
			} `json:"snippet"`
		} `json:"items"`
	}
	if err := a.do(ctx, http.MethodGet, "/videos?"+q.Encode(), &resp); err != nil {
		return LikesPage{}, err
	}
	p := LikesPage{Next: resp.NextPageToken, Total: resp.PageInfo.TotalResults}
	for _, it := range resp.Items {
		p.Videos = append(p.Videos, LikedVideo{
			ID: it.ID, Title: it.Snippet.Title, Channel: it.Snippet.ChannelTitle, CategoryID: it.Snippet.CategoryID,
		})
	}
	return p, nil
}

// Like adds a video to your likes. It costs 50 quota units.
func (a *Account) Like(ctx context.Context, videoID string) error {
	q := url.Values{"id": {videoID}, "rating": {"like"}}
	return a.do(ctx, http.MethodPost, "/videos/rate?"+q.Encode(), nil)
}

// ChannelName is the name of the signed-in account's channel.
func (a *Account) ChannelName(ctx context.Context) (string, error) {
	var resp struct {
		Items []struct {
			Snippet struct {
				Title string `json:"title"`
			} `json:"snippet"`
		} `json:"items"`
	}
	if err := a.do(ctx, http.MethodGet, "/channels?part=snippet&mine=true", &resp); err != nil {
		return "", err
	}
	if len(resp.Items) == 0 {
		return "(no channel)", nil
	}
	return resp.Items[0].Snippet.Title, nil
}

func (a *Account) do(ctx context.Context, method, path string, out any) error {
	req, err := http.NewRequestWithContext(ctx, method, a.base+path, nil)
	if err != nil {
		return err
	}
	resp, err := a.http.Do(req)
	if err != nil {
		var re *oauth2.RetrieveError
		if errors.As(err, &re) {
			return fmt.Errorf("youtube sign-in expired or was revoked; run `cratecli youtube login` (%s)", re.ErrorCode)
		}
		return err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if resp.StatusCode == http.StatusForbidden && strings.Contains(string(body), "quotaExceeded") {
		return ErrQuotaExceeded
	}
	if resp.StatusCode >= 300 {
		return fmt.Errorf("youtube %s %s: HTTP %d: %s", method, strings.SplitN(path, "?", 2)[0], resp.StatusCode, apiMessage(body))
	}
	if out == nil {
		return nil
	}
	return json.Unmarshal(body, out)
}

func apiMessage(body []byte) string {
	var e struct {
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if json.Unmarshal(body, &e) == nil && e.Error.Message != "" {
		return e.Error.Message
	}
	return strings.TrimSpace(string(body))
}

var watchID = regexp.MustCompile(`(?:youtube\.com/watch\?(?:.*&)?v=|youtu\.be/)([A-Za-z0-9_-]{11})`)

// VideoID extracts the ID from a watch or youtu.be URL ("" if none).
func VideoID(u string) string {
	if m := watchID.FindStringSubmatch(u); m != nil {
		return m[1]
	}
	return ""
}
