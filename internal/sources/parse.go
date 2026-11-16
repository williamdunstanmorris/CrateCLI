package sources

import (
	"bytes"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"html"
	"regexp"
	"strings"

	"github.com/PuerkitoBio/goquery"
)

// --- RSS ---

type rssItem struct {
	Title       string   `xml:"title"`
	Link        string   `xml:"link"`
	Description string   `xml:"description"`
	Content     string   `xml:"http://purl.org/rss/1.0/modules/content/ encoded"`
	Categories  []string `xml:"category"`
	PubDate     string   `xml:"pubDate"`
	Creator     string   `xml:"http://purl.org/dc/elements/1.1/ creator"`
	Thumbnail   struct {
		URL string `xml:"url,attr"`
	} `xml:"http://search.yahoo.com/mrss/ thumbnail"`
}

func parseRSS(b []byte) ([]rssItem, error) {
	var doc struct {
		Channel struct {
			Items []rssItem `xml:"item"`
		} `xml:"channel"`
	}
	// Real-world feeds contain bare "&" and HTML entities; parse leniently.
	dec := xml.NewDecoder(bytes.NewReader(b))
	dec.Strict = false
	dec.Entity = xml.HTMLEntity
	if err := dec.Decode(&doc); err != nil {
		return nil, fmt.Errorf("parse RSS: %w", err)
	}
	for i := range doc.Channel.Items {
		it := &doc.Channel.Items[i]
		it.Title = html.UnescapeString(strings.TrimSpace(it.Title))
		for j, c := range it.Categories {
			it.Categories[j] = strings.TrimSpace(html.UnescapeString(c))
		}
	}
	return doc.Channel.Items, nil
}

// blockBreak matches tags that separate words visually but not in text.
var blockBreak = regexp.MustCompile(`(?i)<(br|/p|/div|/li|/h[1-6])\b[^>]*>`)

// stripTags turns an HTML fragment into plain text.
func stripTags(s string) string {
	s = blockBreak.ReplaceAllString(s, " $0")
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(s))
	if err != nil {
		return s
	}
	return strings.Join(strings.Fields(doc.Text()), " ")
}

// --- Nuxt payloads ---
//
// Nuxt 3 sites embed the server-rendered page data as a "devalue" payload in
// <script id="__NUXT_DATA__">: a flat JSON array where objects and arrays
// hold indexes into that same array. This is the data the page itself is
// built from, so reading it is reading the public page.

var nuxtScript = regexp.MustCompile(`(?s)<script[^>]*id="__NUXT_DATA__"[^>]*>(.*?)</script>`)

// nuxtData decodes the page payload and returns its "data" object.
func nuxtData(page []byte) (map[string]any, error) {
	m := nuxtScript.FindSubmatch(page)
	if m == nil {
		return nil, errors.New("page has no embedded data (site layout may have changed)")
	}
	var arr []any
	dec := json.NewDecoder(bytes.NewReader(m[1]))
	dec.UseNumber()
	if err := dec.Decode(&arr); err != nil {
		return nil, fmt.Errorf("decode page data: %w", err)
	}
	root, ok := devalue(arr, 0, 0).(map[string]any)
	if !ok {
		return nil, errors.New("unexpected page data shape")
	}
	data, _ := root["data"].(map[string]any)
	if data == nil {
		return nil, errors.New("page data has no data section")
	}
	return data, nil
}

func devalue(arr []any, idx any, depth int) any {
	i, ok := asIndex(idx)
	if !ok || i < 0 || i >= len(arr) || depth > 40 {
		return nil
	}
	switch v := arr[i].(type) {
	case []any:
		if len(v) > 0 {
			if tag, ok := v[0].(string); ok {
				switch tag {
				case "ShallowReactive", "Reactive", "Ref", "ShallowRef", "EmptyRef", "EmptyShallowRef":
					if len(v) > 1 {
						return devalue(arr, v[1], depth+1)
					}
					return nil
				case "Set":
					out := make([]any, 0, len(v)-1)
					for _, x := range v[1:] {
						out = append(out, devalue(arr, x, depth+1))
					}
					return out
				case "Date":
					if len(v) > 1 {
						return v[1]
					}
					return nil
				default:
					return nil
				}
			}
		}
		out := make([]any, len(v))
		for j, x := range v {
			out[j] = devalue(arr, x, depth+1)
		}
		return out
	case map[string]any:
		out := make(map[string]any, len(v))
		for k, x := range v {
			out[k] = devalue(arr, x, depth+1)
		}
		return out
	case json.Number:
		if f, err := v.Float64(); err == nil {
			return f
		}
		return v.String()
	default:
		return v
	}
}

func asIndex(v any) (int, bool) {
	switch n := v.(type) {
	case json.Number:
		i, err := n.Int64()
		return int(i), err == nil
	case int:
		return n, true
	}
	return 0, false
}

// Helpers for walking decoded JSON.

func obj(v any, path ...string) map[string]any {
	for _, p := range path {
		m, _ := v.(map[string]any)
		if m == nil {
			return nil
		}
		v = m[p]
	}
	m, _ := v.(map[string]any)
	return m
}

func str(m map[string]any, key string) string {
	if m == nil {
		return ""
	}
	switch v := m[key].(type) {
	case string:
		return strings.TrimSpace(v)
	case float64:
		return strings.TrimSuffix(fmt.Sprintf("%f", v), ".000000")
	}
	return ""
}

func list(m map[string]any, key string) []any {
	if m == nil {
		return nil
	}
	l, _ := m[key].([]any)
	return l
}

// slugWords turns "hiroshi-yoshimura" into "Hiroshi Yoshimura".
func slugWords(s string) string {
	parts := strings.FieldsFunc(s, func(r rune) bool { return r == '-' || r == '_' })
	for i, p := range parts {
		if p != "" {
			parts[i] = strings.ToUpper(p[:1]) + p[1:]
		}
	}
	return strings.Join(parts, " ")
}

var slugRe = regexp.MustCompile(`[^a-z0-9]+`)

func slugify(s string) string {
	s = strings.ReplaceAll(strings.ToLower(s), "&", " and ")
	return strings.Trim(slugRe.ReplaceAllString(s, "-"), "-")
}
