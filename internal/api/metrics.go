package api

import (
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"sync"
	"time"
)

// metrics is a tiny Prometheus text-format exporter: request counts and
// durations per route, and digest runs per status. Small enough to read
// in one go; swap in prometheus/client_golang if it ever needs more.
type metrics struct {
	mu       sync.Mutex
	requests map[[2]string]int  // {route, status} → count
	seconds  map[string]float64 // route → total seconds
	runs     map[string]int     // digest run status → count
}

func (m *metrics) request(route string, status int, d time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.requests == nil {
		m.requests, m.seconds = map[[2]string]int{}, map[string]float64{}
	}
	m.requests[[2]string{route, strconv.Itoa(status)}]++
	m.seconds[route] += d.Seconds()
}

func (m *metrics) digestRun(status string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.runs == nil {
		m.runs = map[string]int{}
	}
	m.runs[status]++
}

func (m *metrics) serve(w http.ResponseWriter, _ *http.Request) {
	m.mu.Lock()
	defer m.mu.Unlock()
	w.Header().Set("Content-Type", "text/plain; version=0.0.4")
	fmt.Fprintln(w, "# HELP cratecli_http_requests_total HTTP requests by route and status.")
	fmt.Fprintln(w, "# TYPE cratecli_http_requests_total counter")
	keys := make([][2]string, 0, len(m.requests))
	for k := range m.requests {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool { return keys[i][0]+keys[i][1] < keys[j][0]+keys[j][1] })
	for _, k := range keys {
		fmt.Fprintf(w, "cratecli_http_requests_total{route=%q,status=%q} %d\n", k[0], k[1], m.requests[k])
	}
	fmt.Fprintln(w, "# HELP cratecli_http_request_seconds_total Time spent serving requests, by route.")
	fmt.Fprintln(w, "# TYPE cratecli_http_request_seconds_total counter")
	routes := make([]string, 0, len(m.seconds))
	for r := range m.seconds {
		routes = append(routes, r)
	}
	sort.Strings(routes)
	for _, r := range routes {
		fmt.Fprintf(w, "cratecli_http_request_seconds_total{route=%q} %g\n", r, m.seconds[r])
	}
	fmt.Fprintln(w, "# HELP cratecli_digest_runs_total Digest runs by status since start.")
	fmt.Fprintln(w, "# TYPE cratecli_digest_runs_total counter")
	for _, st := range []string{"sent", "skipped", "failed", "dry-run"} {
		fmt.Fprintf(w, "cratecli_digest_runs_total{status=%q} %d\n", st, m.runs[st])
	}
}
