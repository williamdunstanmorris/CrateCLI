package styles

import (
	"sort"
	"strings"
)

type Genre struct {
	Name   string
	Styles []string
}

// Style is a style name and every genre it appears under (e.g. Dub is under
// both Electronic and Reggae).
type Style struct {
	Name   string   `json:"style"`
	Genres []string `json:"genres"`
}

// Genres returns the genre names in display order.
func Genres() []string {
	out := make([]string, len(Taxonomy))
	for i, g := range Taxonomy {
		out[i] = g.Name
	}
	return out
}

// All returns every unique style, sorted by name.
func All() []Style {
	idx := map[string]*Style{}
	var order []string
	for _, g := range Taxonomy {
		for _, s := range g.Styles {
			if st, ok := idx[s]; ok {
				st.Genres = append(st.Genres, g.Name)
				continue
			}
			idx[s] = &Style{Name: s, Genres: []string{g.Name}}
			order = append(order, s)
		}
	}
	sort.Slice(order, func(i, j int) bool { return strings.ToLower(order[i]) < strings.ToLower(order[j]) })
	out := make([]Style, len(order))
	for i, n := range order {
		out[i] = *idx[n]
	}
	return out
}

// Filter returns styles whose name contains term (case-insensitive) and,
// if genre is set, that belong to that genre.
func Filter(term, genre string) []Style {
	term = norm(term)
	var out []Style
	for _, s := range All() {
		if term != "" && !strings.Contains(norm(s.Name), term) {
			continue
		}
		if genre != "" && !contains(s.Genres, genre) {
			continue
		}
		out = append(out, s)
	}
	return out
}

// CanonicalStyle returns the properly-cased style name for s.
func CanonicalStyle(s string) (string, bool) {
	n := norm(s)
	for _, st := range All() {
		if norm(st.Name) == n {
			return st.Name, true
		}
	}
	return s, false
}

// CanonicalGenre returns the properly-cased genre name for g. It accepts
// shorthand such as "funk", "soul", "folk", "world" or "country".
func CanonicalGenre(g string) (string, bool) {
	n := norm(g)
	for _, name := range Genres() {
		if norm(name) == n {
			return name, true
		}
	}
	for _, name := range Genres() {
		for _, part := range strings.FieldsFunc(norm(name), func(r rune) bool { return r == '/' || r == ',' || r == '&' }) {
			if strings.TrimSpace(part) == n {
				return name, true
			}
		}
	}
	return g, false
}

// Suggest returns up to 5 known styles close to s: substring matches first,
// then small edit-distance typos.
func Suggest(s string) []string {
	n := norm(s)
	type cand struct {
		name string
		dist int
	}
	var cs []cand
	for _, st := range All() {
		sn := norm(st.Name)
		d := levenshtein(n, sn)
		if strings.Contains(sn, n) {
			d = 0
		}
		if d <= 2+len(n)/6 {
			cs = append(cs, cand{st.Name, d})
		}
	}
	sort.SliceStable(cs, func(i, j int) bool { return cs[i].dist < cs[j].dist })
	var out []string
	for i := 0; i < len(cs) && i < 5; i++ {
		out = append(out, cs[i].name)
	}
	return out
}

func norm(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	return strings.NewReplacer("-", " ", "_", " ", "'", "", "’", "").Replace(s)
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if strings.EqualFold(x, s) {
			return true
		}
	}
	return false
}

// Distance is the edit distance between two strings.
func Distance(a, b string) int { return levenshtein(a, b) }

func levenshtein(a, b string) int {
	ra, rb := []rune(a), []rune(b)
	prev := make([]int, len(rb)+1)
	cur := make([]int, len(rb)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(ra); i++ {
		cur[0] = i
		for j := 1; j <= len(rb); j++ {
			cost := 1
			if ra[i-1] == rb[j-1] {
				cost = 0
			}
			cur[j] = min(prev[j]+1, cur[j-1]+1, prev[j-1]+cost)
		}
		prev, cur = cur, prev
	}
	return prev[len(rb)]
}
