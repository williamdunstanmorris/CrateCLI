package core

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"go.yaml.in/yaml/v3"
)

// Limit is how many records a dig returns: a number, or All.
//
// It works as a command-line flag (it implements pflag.Value: String, Set,
// Type) and as JSON, where it's either a number or the string "all".
type Limit struct {
	N   int
	All bool
}

// DefaultLimit is 10 records.
var DefaultLimit = Limit{N: 10}

func (l *Limit) String() string {
	if l.All {
		return "all"
	}
	return strconv.Itoa(l.N)
}

// Set parses "5" or "all".
func (l *Limit) Set(s string) error {
	if strings.EqualFold(s, "all") {
		l.All = true
		return nil
	}
	n, err := strconv.Atoi(s)
	if err != nil || n < 1 {
		return fmt.Errorf(`want a number of at least 1, or "all"`)
	}
	l.N, l.All = n, false
	return nil
}

// Type names the flag's value in --help.
func (l *Limit) Type() string { return "n|all" }

// Of is how many of avail records to take.
func (l Limit) Of(avail int) int {
	if l.All {
		return avail
	}
	return min(l.N, avail)
}

// IsZero reports an unset limit.
func (l Limit) IsZero() bool { return !l.All && l.N == 0 }

// MarshalJSON writes 10 or "all".
func (l Limit) MarshalJSON() ([]byte, error) {
	if l.All {
		return []byte(`"all"`), nil
	}
	return json.Marshal(l.N)
}

// UnmarshalJSON reads 10 or "all".
func (l *Limit) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err == nil {
		return l.Set(s)
	}
	var n int
	if err := json.Unmarshal(b, &n); err != nil {
		return fmt.Errorf(`limit: want a number or "all"`)
	}
	if n < 0 {
		return fmt.Errorf("limit: must not be negative")
	}
	l.N, l.All = n, false
	return nil
}

// FetchSize is how many candidates to ask a source for. Hiding seen
// records throws some away, so tracked digs ask for more than they show.
func FetchSize(l Limit, track, includeSeen bool) int {
	switch {
	case l.All:
		return 100
	case track && !includeSeen:
		return min(max(l.N*3, 30), 100)
	}
	return l.N
}

// UnmarshalYAML reads `limit: 10` or `limit: all` in digest files.
func (l *Limit) UnmarshalYAML(value *yaml.Node) error {
	if err := l.Set(value.Value); err != nil {
		return fmt.Errorf("line %d: limit: %w", value.Line, err)
	}
	return nil
}

// MarshalYAML writes 10 or all.
func (l Limit) MarshalYAML() (any, error) {
	if l.All {
		return "all", nil
	}
	return l.N, nil
}
