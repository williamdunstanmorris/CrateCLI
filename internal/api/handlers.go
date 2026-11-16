package api

import (
	"errors"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/williamdunstanmorris/cratecli/internal/core"
	"github.com/williamdunstanmorris/cratecli/internal/crate"
	"github.com/williamdunstanmorris/cratecli/internal/digest"
	"github.com/williamdunstanmorris/cratecli/internal/library"
	"github.com/williamdunstanmorris/cratecli/internal/sources"
	"github.com/williamdunstanmorris/cratecli/internal/ytsync"
)

// Record is a record in API responses: its number in the list, a stable
// ID, and all of crate.Record's fields (embedding flattens them in JSON).
type Record struct {
	N  int    `json:"n"`
	ID string `json:"id"`
	crate.Record
}

func numbered(recs []crate.Record) []Record {
	out := make([]Record, len(recs))
	for i, r := range recs {
		out[i] = Record{N: i + 1, ID: r.ID(), Record: r}
	}
	return out
}

// ---------------------------------------------------------------- sources

func (s *Server) listSources(w http.ResponseWriter, _ *http.Request) {
	var out []sources.Info
	for _, src := range sources.Sorted(s.Service.Sources) {
		out = append(out, src.Info())
	}
	writeJSON(w, http.StatusOK, map[string]any{"sources": out})
}

func (s *Server) getSource(w http.ResponseWriter, r *http.Request) {
	src, ok := s.Service.Sources[strings.ToLower(r.PathValue("name"))]
	if !ok {
		problem(w, http.StatusNotFound, "no such source", r.PathValue("name"))
		return
	}
	writeJSON(w, http.StatusOK, src.Info())
}

// ---------------------------------------------------------------- digs

// DigBody is POST /v1/digs: a core.DigRequest plus whether to keep it in
// history (default: yes, like the CLI).
type DigBody struct {
	core.DigRequest
	// Record logs the dig (so its numbers work with POST /v1/saved) and,
	// for from/random digs, marks its records seen. Default true.
	Record *bool `json:"record,omitempty"`
}

// DigResponse is what a dig returns.
type DigResponse struct {
	DigID   int64      `json:"dig_id,omitempty"`
	Stats   core.Stats `json:"stats"`
	Records []Record   `json:"records"`
}

func (s *Server) createDig(w http.ResponseWriter, r *http.Request) {
	var body DigBody
	if err := decode(r, &body); err != nil {
		problem(w, http.StatusBadRequest, "bad request", err.Error())
		return
	}
	if err := s.Service.Validate(body.DigRequest); err != nil {
		fail(w, bad(err))
		return
	}
	res, err := s.Service.Dig(r.Context(), body.DigRequest)
	if err != nil {
		fail(w, err)
		return
	}
	out := DigResponse{Stats: res.Stats, Records: numbered(res.Records)}
	if body.Record == nil || *body.Record {
		label := "api: " + describe(body.DigRequest)
		if out.DigID, err = s.Service.Record(r.Context(), label, res.Records, res.Track); err != nil {
			fail(w, err)
			return
		}
	}
	writeJSON(w, http.StatusOK, out)
}

func describe(req core.DigRequest) string {
	if req.Search != nil {
		return "search"
	}
	return "from " + strings.Join(req.From, " ")
}

func (s *Server) listDigs(w http.ResponseWriter, r *http.Request) {
	lib, ok := s.library(w)
	if !ok {
		return
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	digs, err := lib.Digs(r.Context(), limit)
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"digs": digs})
}

func (s *Server) getDig(w http.ResponseWriter, r *http.Request) {
	lib, ok := s.library(w)
	if !ok {
		return
	}
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		problem(w, http.StatusBadRequest, "bad dig id", r.PathValue("id"))
		return
	}
	d, recs, err := lib.GetDig(r.Context(), id)
	if errors.Is(err, library.ErrNoDigs) {
		problem(w, http.StatusNotFound, "no such dig", r.PathValue("id"))
		return
	}
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"dig": d, "records": numbered(recs)})
}

// ---------------------------------------------------------------- saved

func (s *Server) listSaved(w http.ResponseWriter, r *http.Request) {
	lib, ok := s.library(w)
	if !ok {
		return
	}
	recs, err := lib.Saved(r.Context())
	if err != nil {
		fail(w, err)
		return
	}
	q := r.URL.Query()
	var keep []crate.Record
	for _, rec := range recs {
		if o := q.Get("origin"); o != "" && !strings.EqualFold(rec.Origin, o) {
			continue
		}
		if src := q.Get("source"); src != "" {
			name, _, _ := strings.Cut(rec.Source, ":")
			if !strings.EqualFold(name, src) && !strings.EqualFold(rec.Source, src) {
				continue
			}
		}
		keep = append(keep, rec)
	}
	writeJSON(w, http.StatusOK, map[string]any{"records": numbered(keep)})
}

// SaveBody is POST /v1/saved: records from a past dig, by number.
type SaveBody struct {
	DigID   int64  `json:"dig_id"`
	Numbers []int  `json:"numbers"`
	Note    string `json:"note,omitempty"`
}

func (s *Server) save(w http.ResponseWriter, r *http.Request) {
	lib, ok := s.library(w)
	if !ok {
		return
	}
	var body SaveBody
	if err := decode(r, &body); err != nil {
		problem(w, http.StatusBadRequest, "bad request", err.Error())
		return
	}
	if body.DigID == 0 || len(body.Numbers) == 0 {
		problem(w, http.StatusBadRequest, "bad request", "need dig_id and numbers")
		return
	}
	_, recs, err := lib.GetDig(r.Context(), body.DigID)
	if errors.Is(err, library.ErrNoDigs) {
		problem(w, http.StatusNotFound, "no such dig", strconv.FormatInt(body.DigID, 10))
		return
	}
	if err != nil {
		fail(w, err)
		return
	}
	var picked []crate.Record
	for _, n := range body.Numbers {
		if n < 1 || n > len(recs) {
			problem(w, http.StatusBadRequest, "bad request", fmt.Sprintf("no record %d in dig %d (1-%d)", n, body.DigID, len(recs)))
			return
		}
		picked = append(picked, recs[n-1])
	}
	res, err := lib.Save(r.Context(), picked, body.Note)
	if err != nil {
		fail(w, err)
		return
	}
	type saved struct {
		ID     string `json:"id"`
		Query  string `json:"title"`
		Result string `json:"result"`
	}
	var out []saved
	for i, rec := range picked {
		out = append(out, saved{rec.ID(), rec.Query(),
			map[library.SaveResult]string{library.Added: "saved", library.AlreadySaved: "already saved", library.NoteUpdated: "note updated"}[res[i]]})
	}
	writeJSON(w, http.StatusOK, map[string]any{"saved": out})
}

func (s *Server) unsave(w http.ResponseWriter, r *http.Request) {
	lib, ok := s.library(w)
	if !ok {
		return
	}
	recs, err := lib.Saved(r.Context())
	if err != nil {
		fail(w, err)
		return
	}
	for _, rec := range recs {
		if rec.ID() == r.PathValue("id") {
			if _, err := lib.Unsave(r.Context(), []crate.Record{rec}); err != nil {
				fail(w, err)
				return
			}
			w.WriteHeader(http.StatusNoContent)
			return
		}
	}
	problem(w, http.StatusNotFound, "not saved", r.PathValue("id"))
}

// ---------------------------------------------------------------- seen

func (s *Server) seenStats(w http.ResponseWriter, r *http.Request) {
	lib, ok := s.library(w)
	if !ok {
		return
	}
	st, err := lib.Stats(r.Context())
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, st)
}

func (s *Server) undoSeen(w http.ResponseWriter, r *http.Request) {
	lib, ok := s.library(w)
	if !ok {
		return
	}
	d, n, err := lib.UndoSeen(r.Context())
	if errors.Is(err, library.ErrNoDigs) {
		problem(w, http.StatusConflict, "nothing to undo", "")
		return
	}
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"dig": d, "unseen": n})
}

// ---------------------------------------------------------------- youtube

func (s *Server) syncYouTube(w http.ResponseWriter, r *http.Request) {
	lib, ok := s.library(w)
	if !ok {
		return
	}
	var body struct {
		Full   bool `json:"full"`
		DryRun bool `json:"dry_run"`
	}
	if err := decode(r, &body); err != nil {
		problem(w, http.StatusBadRequest, "bad request", err.Error())
		return
	}
	if s.YouTube == nil {
		problem(w, http.StatusServiceUnavailable, "youtube not configured", "")
		return
	}
	acct, err := s.YouTube(r.Context())
	if err != nil {
		problem(w, http.StatusServiceUnavailable, "youtube not signed in", err.Error())
		return
	}
	res, err := ytsync.Sync(r.Context(), acct, lib, ytsync.Options{Full: body.Full, DryRun: body.DryRun})
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, res)
}

// ---------------------------------------------------------------- digests

func (s *Server) digestFile(w http.ResponseWriter) (*digest.File, bool) {
	if s.Digests == nil {
		problem(w, http.StatusServiceUnavailable, "digests not configured", "")
		return nil, false
	}
	f, err := s.Digests()
	if err != nil {
		problem(w, http.StatusServiceUnavailable, "digests.yaml unavailable", err.Error())
		return nil, false
	}
	return f, true
}

func (s *Server) listDigests(w http.ResponseWriter, _ *http.Request) {
	f, ok := s.digestFile(w)
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"timezone": f.Timezone, "digests": f.Digests})
}

func (s *Server) previewDigest(w http.ResponseWriter, r *http.Request) {
	f, ok := s.digestFile(w)
	if !ok {
		return
	}
	d, err := f.Find(r.PathValue("name"))
	if err != nil {
		problem(w, http.StatusNotFound, "no such digest", err.Error())
		return
	}
	res, err := s.runner(f).Send(r.Context(), d, digest.SendOptions{DryRun: true})
	if err != nil {
		fail(w, err)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(res.Message.HTML))
}

// digestRuns answers "was this digest sent on ?date=" — what a Mac in
// fallback mode asks before sending.
func (s *Server) digestRuns(w http.ResponseWriter, r *http.Request) {
	lib, ok := s.library(w)
	if !ok {
		return
	}
	runs, err := lib.Runs(r.Context(), r.PathValue("name"), r.URL.Query().Get("date"), 50)
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"runs": nonNil(runs)})
}

func (s *Server) listRuns(w http.ResponseWriter, r *http.Request) {
	lib, ok := s.library(w)
	if !ok {
		return
	}
	q := r.URL.Query()
	limit, _ := strconv.Atoi(q.Get("limit"))
	runs, err := lib.Runs(r.Context(), q.Get("digest"), q.Get("date"), limit)
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"runs": nonNil(runs)})
}

// RunBody is POST /v1/digest-runs: send one digest now, or every due one.
type RunBody struct {
	Digest string   `json:"digest,omitempty"`
	Due    bool     `json:"due,omitempty"`
	To     []string `json:"to,omitempty"`
	DryRun bool     `json:"dry_run,omitempty"`
}

func (s *Server) createRun(w http.ResponseWriter, r *http.Request) {
	f, ok := s.digestFile(w)
	if !ok {
		return
	}
	var body RunBody
	if err := decode(r, &body); err != nil {
		problem(w, http.StatusBadRequest, "bad request", err.Error())
		return
	}
	if (body.Digest == "") == !body.Due {
		problem(w, http.StatusBadRequest, "bad request", `give either "digest": "<name>" or "due": true`)
		return
	}
	if s.Sender == nil && !body.DryRun {
		problem(w, http.StatusServiceUnavailable, "mail not configured", "set CRATECLI_SMTP_HOST etc.")
		return
	}
	run := s.runner(f)
	var todo []digest.Digest
	if body.Due {
		due, err := run.Due(r.Context())
		if err != nil {
			fail(w, err)
			return
		}
		todo = due
	} else {
		d, err := f.Find(body.Digest)
		if err != nil {
			problem(w, http.StatusNotFound, "no such digest", err.Error())
			return
		}
		todo = []digest.Digest{d}
	}
	var runs []library.DigestRun
	status := http.StatusOK
	for _, d := range todo {
		res, err := run.Send(r.Context(), d, digest.SendOptions{To: body.To, DryRun: body.DryRun})
		if err != nil {
			status = http.StatusBadGateway
			runs = append(runs, library.DigestRun{Digest: d.Name, Status: library.RunFailed, Message: err.Error(), FinishedAt: time.Now()})
			continue
		}
		s.metrics.digestRun(res.Run.Status)
		runs = append(runs, res.Run)
	}
	writeJSON(w, status, map[string]any{"runs": nonNil(runs)})
}

func (s *Server) runner(f *digest.File) *digest.Runner {
	rn := digest.NewRunner(s.Service, f, s.Sender)
	if h, err := os.Hostname(); err == nil {
		rn.Host = h
	}
	return rn
}

func (s *Server) library(w http.ResponseWriter) (*library.Library, bool) {
	if s.Service.Library == nil {
		problem(w, http.StatusServiceUnavailable, "no library", "the server has no database")
		return nil, false
	}
	return s.Service.Library, true
}

func nonNil[T any](s []T) []T {
	if s == nil {
		return []T{}
	}
	return s
}
