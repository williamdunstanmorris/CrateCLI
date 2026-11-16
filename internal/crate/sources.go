package crate

import (
	"context"
	"fmt"
	"math/rand/v2"
	"strconv"
	"strings"

	"github.com/williamdunstanmorris/cratecli/internal/discogs"
)

const discogsWeb = "https://www.discogs.com"

// randomPage picks a page in [1, pages], respecting an API-imposed ceiling.
func randomPage(pages, ceiling int) int {
	if ceiling > 0 && pages > ceiling {
		pages = ceiling
	}
	if pages <= 1 {
		return 1
	}
	return rand.IntN(pages) + 1
}

// sample returns up to n items, shuffled when random is set.
func sample[T any](items []T, n int, random bool) []T {
	if random {
		rand.Shuffle(len(items), func(i, j int) { items[i], items[j] = items[j], items[i] })
	}
	if n > 0 && len(items) > n {
		items = items[:n]
	}
	return items
}

// FromSearch runs a database search. With random set it jumps to a random
// page of the result set and shuffles, which makes broad queries
// (e.g. --style "Dub Techno") into a lucky dip.
func FromSearch(ctx context.Context, c *discogs.Client, p discogs.SearchParams, limit int, random bool) ([]Record, error) {
	if p.Type == "" {
		p.Type = "release"
	}
	if random {
		p.PerPage = 50
		p.Page = 1
		first, err := c.Search(ctx, p)
		if err != nil {
			return nil, err
		}
		// Discogs already caps "pages" at the 10k result ceiling.
		p.Page = randomPage(first.Pagination.Pages, 0)
		resp := first
		if p.Page != 1 {
			if resp, err = c.Search(ctx, p); err != nil {
				return nil, err
			}
		}
		return searchRecords(sample(resp.Results, limit, true)), nil
	}

	if p.PerPage == 0 {
		p.PerPage = min(limit, 100)
	}
	resp, err := c.Search(ctx, p)
	if err != nil {
		return nil, err
	}
	return searchRecords(sample(resp.Results, limit, false)), nil
}

func searchRecords(rs []discogs.SearchResult) []Record {
	out := make([]Record, 0, len(rs))
	for _, r := range rs {
		artist, title := SplitTitle(r.Title)
		rec := Record{
			ReleaseID:  r.ID,
			Artist:     artist,
			Title:      title,
			CatNo:      r.CatNo,
			Format:     strings.Join(r.Format, ", "),
			Year:       r.Year,
			Country:    r.Country,
			Genres:     r.Genre,
			Styles:     r.Style,
			Want:       r.Community.Want,
			Have:       r.Community.Have,
			DiscogsURL: discogsWeb + r.URI,
			Source:     "search",
		}
		if len(r.Label) > 0 {
			rec.Label = r.Label[0]
		}
		out = append(out, rec)
	}
	return out
}

// Inventory sort keys that give useful different "slices" of a big shop.
var inventorySorts = []string{"listed", "price", "artist", "label", "catno", "item"}

// SellerOptions controls FromSeller.
type SellerOptions struct {
	Sort   string // default "listed"
	Order  string // default "desc" (newest first)
	Page   int
	Limit  int
	Random bool
}

// FromSeller pulls records from a Discogs seller's inventory. By default it
// returns the newest listings. With Random it picks a random sort key, order
// and page, which reaches parts of big inventories that the 100-page cap
// would otherwise hide.
func FromSeller(ctx context.Context, c *discogs.Client, username string, o SellerOptions) ([]Record, error) {
	p := discogs.InventoryParams{Sort: o.Sort, SortOrder: o.Order, Page: o.Page, PerPage: min(o.Limit, 100)}
	if p.Sort == "" {
		p.Sort = "listed"
	}
	if p.SortOrder == "" {
		p.SortOrder = "desc"
	}

	if o.Random {
		if o.Sort == "" {
			p.Sort = inventorySorts[rand.IntN(len(inventorySorts))]
		}
		if o.Order == "" && rand.IntN(2) == 0 {
			p.SortOrder = "asc"
		}
		p.PerPage, p.Page = 100, 1
		first, err := c.Inventory(ctx, username, p)
		if err != nil {
			return nil, err
		}
		// Other users' inventories can't be paged past 100.
		p.Page = randomPage(first.Pagination.Pages, 100)
		resp := first
		if p.Page != 1 {
			if resp, err = c.Inventory(ctx, username, p); err != nil {
				return nil, err
			}
		}
		return listingRecords(sample(resp.Listings, o.Limit, true), username), nil
	}

	resp, err := c.Inventory(ctx, username, p)
	if err != nil {
		return nil, err
	}
	if resp.Pagination.Items == 0 {
		return nil, fmt.Errorf("seller %q has no items for sale on Discogs", username)
	}
	return listingRecords(sample(resp.Listings, o.Limit, false), username), nil
}

func listingRecords(ls []discogs.Listing, seller string) []Record {
	out := make([]Record, 0, len(ls))
	for _, l := range ls {
		r := l.Release
		rec := Record{
			ReleaseID:  r.ID,
			Artist:     r.Artist,
			Title:      r.Title,
			Label:      r.Label,
			CatNo:      r.CatNo,
			Format:     r.Format,
			Want:       r.Stats.Community.InWantlist,
			Have:       r.Stats.Community.InCollection,
			DiscogsURL: fmt.Sprintf("%s/release/%d", discogsWeb, r.ID),
			Source:     "seller:" + seller,
			ListingURL: l.URI,
			Condition:  l.Condition,
			Listed:     l.Posted,
		}
		if r.Year > 0 {
			rec.Year = strconv.Itoa(r.Year)
		}
		if l.Price.Value > 0 {
			rec.Price = fmt.Sprintf("%.2f %s", l.Price.Value, l.Price.Currency)
		}
		out = append(out, rec)
	}
	return out
}

// FromWants pulls records from a user's public wantlist: a way to dig
// through someone else's taste.
func FromWants(ctx context.Context, c *discogs.Client, username string, limit int, random bool) ([]Record, error) {
	page := 1
	if random {
		first, err := c.Wants(ctx, username, 1, 100)
		if err != nil {
			return nil, err
		}
		page = randomPage(first.Pagination.Pages, 0)
		if page == 1 {
			return wantRecords(sample(first.Wants, limit, true), username), nil
		}
	}
	perPage := min(limit, 100)
	if random {
		perPage = 100
	}
	resp, err := c.Wants(ctx, username, page, perPage)
	if err != nil {
		return nil, err
	}
	return wantRecords(sample(resp.Wants, limit, random), username), nil
}

func wantRecords(ws []discogs.Want, user string) []Record {
	out := make([]Record, 0, len(ws))
	for _, w := range ws {
		b := w.BasicInformation
		names := make([]string, 0, len(b.Artists))
		for _, a := range b.Artists {
			names = append(names, a.Name)
		}
		rec := Record{
			ReleaseID:  b.ID,
			Artist:     strings.Join(names, ", "),
			Title:      b.Title,
			Genres:     b.Genres,
			Styles:     b.Styles,
			DiscogsURL: fmt.Sprintf("%s/release/%d", discogsWeb, b.ID),
			Source:     "wants:" + user,
		}
		if b.Year > 0 {
			rec.Year = strconv.Itoa(b.Year)
		}
		if len(b.Labels) > 0 {
			rec.Label, rec.CatNo = b.Labels[0].Name, b.Labels[0].CatNo
		}
		if len(b.Formats) > 0 {
			rec.Format = strings.Join(append([]string{b.Formats[0].Name}, b.Formats[0].Descriptions...), ", ")
		}
		out = append(out, rec)
	}
	return out
}

// FromRelease builds a single record from a release ID.
func FromRelease(id int) []Record {
	return []Record{{
		ReleaseID:  id,
		DiscogsURL: fmt.Sprintf("%s/release/%d", discogsWeb, id),
		Source:     "release",
	}}
}
