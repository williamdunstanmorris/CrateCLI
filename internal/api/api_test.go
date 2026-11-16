package api

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/williamdunstanmorris/cratecli/internal/core"
	"github.com/williamdunstanmorris/cratecli/internal/crate"
	"github.com/williamdunstanmorris/cratecli/internal/digest"
	"github.com/williamdunstanmorris/cratecli/internal/discogs"
	"github.com/williamdunstanmorris/cratecli/internal/library"
	"github.com/williamdunstanmorris/cratecli/internal/mail"
	"github.com/williamdunstanmorris/cratecli/internal/sources"
)

type fakeShop struct{}

func (fakeShop) Info() sources.Info {
	return sources.Info{Name: "fake", Kind: sources.KindShop, Sections: []sources.Section{{Name: "new"}}}
}
func (fakeShop) Fetch(context.Context, sources.Request) ([]crate.Record, error) {
	return []crate.Record{{Artist: "Chez Damier", Title: "Can You Feel It"}, {Artist: "Kerrie", Title: "Pressure Signal"}}, nil
}

type memSender struct{ n int }

func (m *memSender) Send(context.Context, mail.Message) error { m.n++; return nil }

func newTestServer(t *testing.T, token string) (*httptest.Server, *memSender) {
	t.Helper()
	lib, err := library.Open(filepath.Join(t.TempDir(), "lib.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { lib.Close() })
	svc := &core.Service{Discogs: discogs.New("", "test"), Library: lib,
		Sources: map[string]sources.Source{"fake": fakeShop{}}}
	sender := &memSender{}
	file := &digest.File{
		Timezone: "UTC", From: "Crate <music@example.com>",
		Recipients: map[string]string{"me": "me@example.com"},
		Digests: []digest.Digest{{Name: "tuesday", Days: []string{"daily"}, At: "00:00", To: []string{"me"},
			Sections: []digest.Section{{Title: "Fake", Dig: core.DigRequest{From: []string{"fake"}, Fast: true}}}}},
	}
	s := &Server{Service: svc, Sender: sender, Token: token,
		Digests: func() (*digest.File, error) { return file, nil }}
	ts := httptest.NewServer(s.Handler())
	t.Cleanup(ts.Close)
	return ts, sender
}

func call(t *testing.T, ts *httptest.Server, method, path, body string, out any) int {
	t.Helper()
	req, _ := http.NewRequest(method, ts.URL+path, strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer secret")
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	if out != nil {
		if err := json.Unmarshal(b, out); err != nil {
			t.Fatalf("%s %s: %v\n%s", method, path, err, b)
		}
	}
	return resp.StatusCode
}

func TestDigSaveAndDigest(t *testing.T) {
	ts, sender := newTestServer(t, "secret")

	var dig DigResponse
	if code := call(t, ts, "POST", "/v1/digs", `{"from":["fake"],"fast":true,"limit":"all"}`, &dig); code != 200 {
		t.Fatalf("dig: %d", code)
	}
	if len(dig.Records) != 2 || dig.Records[0].N != 1 || dig.Records[0].Artist != "Chez Damier" || !strings.HasPrefix(dig.Records[0].ID, "rec_") || dig.DigID == 0 {
		t.Fatalf("dig = %+v", dig)
	}
	// Same dig again: everything was seen.
	var again DigResponse
	call(t, ts, "POST", "/v1/digs", `{"from":["fake"],"fast":true}`, &again)
	if len(again.Records) != 0 || again.Stats.HiddenSeen != 2 {
		t.Errorf("second dig = %+v", again)
	}

	body, _ := json.Marshal(SaveBody{DigID: dig.DigID, Numbers: []int{2}, Note: "nice"})
	if code := call(t, ts, "POST", "/v1/saved", string(body), nil); code != 200 {
		t.Fatalf("save: %d", code)
	}
	var saved struct{ Records []Record }
	call(t, ts, "GET", "/v1/saved", "", &saved)
	if len(saved.Records) != 1 || saved.Records[0].Title != "Pressure Signal" || saved.Records[0].MyNote != "nice" {
		t.Fatalf("saved = %+v", saved)
	}
	if code := call(t, ts, "DELETE", "/v1/saved/"+saved.Records[0].ID, "", nil); code != 204 {
		t.Errorf("unsave: %d", code)
	}

	// Digest: nothing new (all seen) → skipped, not sent.
	var runs struct{ Runs []library.DigestRun }
	if code := call(t, ts, "POST", "/v1/digest-runs", `{"digest":"tuesday"}`, &runs); code != 200 {
		t.Fatalf("run: %d", code)
	}
	if len(runs.Runs) != 1 || runs.Runs[0].Status != library.RunSkipped || sender.n != 0 {
		t.Errorf("runs = %+v, sent %d", runs, sender.n)
	}
	call(t, ts, "GET", "/v1/digests/tuesday/runs?date="+runs.Runs[0].Date, "", &runs)
	if len(runs.Runs) != 1 {
		t.Errorf("runs on date = %+v", runs)
	}
}

func TestErrorsAndAuth(t *testing.T) {
	ts, _ := newTestServer(t, "secret")
	var p map[string]any
	if code := call(t, ts, "POST", "/v1/digs", `{"from":["nope"]}`, &p); code != 400 || !strings.Contains(p["detail"].(string), "unknown source") {
		t.Errorf("bad source: %d %v", code, p)
	}
	if code := call(t, ts, "POST", "/v1/digs", `{"form":["fake"]}`, &p); code != 400 {
		t.Errorf("unknown field: %d %v", code, p)
	}
	// No token → 401, but health stays open.
	resp, _ := http.Post(ts.URL+"/v1/digs", "application/json", bytes.NewBufferString(`{}`))
	if resp.StatusCode != 401 {
		t.Errorf("no token: %d", resp.StatusCode)
	}
	resp, _ = http.Get(ts.URL + "/healthz")
	if resp.StatusCode != 200 {
		t.Errorf("healthz: %d", resp.StatusCode)
	}
	resp, _ = http.Get(ts.URL + "/metrics")
	b, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(b), `cratecli_http_requests_total{route="POST /v1/digs",status="400"} 2`) {
		t.Errorf("metrics:\n%s", b)
	}
}
