package discogs

import (
	"context"
	"net/url"
	"strconv"
)

// SearchParams mirrors the documented /database/search parameters.
type SearchParams struct {
	Query        string // q
	Type         string // release, master, artist, label
	Title        string // "Artist - Title" combined
	ReleaseTitle string
	Artist       string
	Label        string
	Genre        string
	Style        string
	Country      string
	Year         string // e.g. "1996"
	Format       string
	CatNo        string
	Track        string
	Credit       string
	// Sort is one of SearchSorts; SortOrder is asc or desc. These aren't in
	// the official docs but the API honours them.
	Sort      string
	SortOrder string
	Page      int
	PerPage   int
}

func (p SearchParams) values() url.Values {
	v := url.Values{}
	set := func(k, s string) {
		if s != "" {
			v.Set(k, s)
		}
	}
	set("q", p.Query)
	set("type", p.Type)
	set("title", p.Title)
	set("release_title", p.ReleaseTitle)
	set("artist", p.Artist)
	set("label", p.Label)
	set("genre", p.Genre)
	set("style", p.Style)
	set("country", p.Country)
	set("year", p.Year)
	set("format", p.Format)
	set("catno", p.CatNo)
	set("track", p.Track)
	set("credit", p.Credit)
	set("sort", p.Sort)
	set("sort_order", p.SortOrder)
	setPaging(v, p.Page, p.PerPage)
	return v
}

func setPaging(v url.Values, page, perPage int) {
	if page > 0 {
		v.Set("page", strconv.Itoa(page))
	}
	if perPage > 0 {
		v.Set("per_page", strconv.Itoa(perPage))
	}
}

// SearchSorts are the sort keys /database/search actually honours (others
// are silently ignored). want/have sort by community wantlist/collection counts.
var SearchSorts = []string{"year", "title", "want", "have"}

func (c *Client) Search(ctx context.Context, p SearchParams) (*SearchResponse, error) {
	var out SearchResponse
	return &out, c.get(ctx, "/database/search", p.values(), &out)
}

// InventoryParams controls a seller inventory request.
// Sort is one of: listed, price, item, artist, label, catno, audio, status, location.
type InventoryParams struct {
	Sort      string
	SortOrder string // asc or desc
	Page      int
	PerPage   int
}

func (c *Client) Inventory(ctx context.Context, username string, p InventoryParams) (*InventoryResponse, error) {
	v := url.Values{}
	v.Set("status", "For Sale")
	if p.Sort != "" {
		v.Set("sort", p.Sort)
	}
	if p.SortOrder != "" {
		v.Set("sort_order", p.SortOrder)
	}
	setPaging(v, p.Page, p.PerPage)
	var out InventoryResponse
	return &out, c.get(ctx, "/users/"+url.PathEscape(username)+"/inventory", v, &out)
}

func (c *Client) Wants(ctx context.Context, username string, page, perPage int) (*WantsResponse, error) {
	v := url.Values{}
	setPaging(v, page, perPage)
	var out WantsResponse
	return &out, c.get(ctx, "/users/"+url.PathEscape(username)+"/wants", v, &out)
}

func (c *Client) Release(ctx context.Context, id int) (*Release, error) {
	var out Release
	return &out, c.get(ctx, "/releases/"+strconv.Itoa(id), nil, &out)
}

// ArtistRelease is one entry from /artists/{id}/releases.
type ArtistRelease struct {
	ID          int    `json:"id"`
	Type        string `json:"type"` // "master" or "release"
	MainRelease int    `json:"main_release"`
	Title       string `json:"title"`
	Artist      string `json:"artist"`
	Role        string `json:"role"` // Main, Appearance, TrackAppearance, Remix, UnofficialRelease
	Year        int    `json:"year"`
	Label       string `json:"label"`
	Format      string `json:"format"`
}

type ArtistReleasesResponse struct {
	Pagination Pagination      `json:"pagination"`
	Releases   []ArtistRelease `json:"releases"`
}

// ArtistReleases lists an artist's releases and masters, oldest first.
func (c *Client) ArtistReleases(ctx context.Context, artistID, page int) (*ArtistReleasesResponse, error) {
	v := url.Values{}
	v.Set("sort", "year")
	v.Set("sort_order", "asc")
	setPaging(v, page, 100)
	var out ArtistReleasesResponse
	return &out, c.get(ctx, "/artists/"+strconv.Itoa(artistID)+"/releases", v, &out)
}

// Master is the subset of /masters/{id} cratecli uses.
type Master struct {
	ID          int     `json:"id"`
	MainRelease int     `json:"main_release"`
	Title       string  `json:"title"`
	Year        int     `json:"year"`
	Videos      []Video `json:"videos"`
}

func (c *Client) Master(ctx context.Context, id int) (*Master, error) {
	var out Master
	return &out, c.get(ctx, "/masters/"+strconv.Itoa(id), nil, &out)
}
