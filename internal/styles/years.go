package styles

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// YearRange is an inclusive span of years.
type YearRange struct {
	From, To int
}

// Param renders the range for Discogs' year= search parameter, which accepts
// either "1995" or "1990-1999".
func (r YearRange) Param() string {
	if r.From == r.To {
		return strconv.Itoa(r.From)
	}
	return fmt.Sprintf("%d-%d", r.From, r.To)
}

func (r YearRange) String() string { return r.Param() }

var (
	reSingle = regexp.MustCompile(`^(\d{4})$`)
	reRange  = regexp.MustCompile(`^(\d{4})\s*(?:-|–|\.\.|to)\s*(\d{4})$`)
	reDecade = regexp.MustCompile(`^(early|mid|late)?\s*'?(\d{2}|\d{4})s$`)
	reSpaced = regexp.MustCompile(`^(\d{4}) (\d{4})$`)
)

// ParseYears understands:
//
//	1995            a single year
//	1990-1999       a range (also 1990..1999, 1990 to 1999)
//	90s, '90s, 1990s
//	early 90s       1990-1993
//	mid 90s         1994-1996
//	late 90s        1997-1999
func ParseYears(s string) (YearRange, error) {
	in := strings.ToLower(strings.TrimSpace(s))
	in = strings.ReplaceAll(in, "-", " ")
	in = strings.Join(strings.Fields(in), " ")
	// Restore a hyphen between two 4-digit years: "1990 1999" -> range.
	if m := reSpaced.FindStringSubmatch(in); m != nil {
		in = m[1] + "-" + m[2]
	}

	switch {
	case reSingle.MatchString(in):
		y, _ := strconv.Atoi(in)
		return check(YearRange{y, y}, s)
	case reRange.MatchString(in):
		m := reRange.FindStringSubmatch(in)
		a, _ := strconv.Atoi(m[1])
		b, _ := strconv.Atoi(m[2])
		if a > b {
			a, b = b, a
		}
		return check(YearRange{a, b}, s)
	case reDecade.MatchString(in):
		m := reDecade.FindStringSubmatch(in)
		start, _ := strconv.Atoi(m[2])
		if len(m[2]) == 2 {
			// '00s-'20s are this century; '30s-'90s are the last one.
			if start <= 29 {
				start += 2000
			} else {
				start += 1900
			}
		}
		if start%10 != 0 {
			return YearRange{}, fmt.Errorf("%q: a decade should end in 0 (e.g. 90s, 1980s)", s)
		}
		r := YearRange{start, start + 9}
		switch m[1] {
		case "early":
			r = YearRange{start, start + 3}
		case "mid":
			r = YearRange{start + 4, start + 6}
		case "late":
			r = YearRange{start + 7, start + 9}
		}
		return check(r, s)
	}
	return YearRange{}, fmt.Errorf("can't read year %q (try 1995, 1990-1999, 90s, late 80s)", s)
}

func check(r YearRange, orig string) (YearRange, error) {
	max := time.Now().Year() + 1
	if r.From < 1860 || r.To > max {
		return r, fmt.Errorf("year %q is outside 1860-%d", orig, max)
	}
	return r, nil
}

// Decades returns the decade ranges from..to inclusive (e.g. 1950s..2020s).
func Decades(from, to int) []YearRange {
	var out []YearRange
	for d := from - from%10; d <= to; d += 10 {
		out = append(out, YearRange{d, d + 9})
	}
	return out
}
