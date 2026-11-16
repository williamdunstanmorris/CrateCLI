# Running cratecli as a service

This is what the container needs and promises: the contract between the app and wherever you run it. How to run it on Kubernetes is deliberately left to you; there are questions at the end to work through.

## The image

```sh
make docker            # builds cratecli:<version> from the Dockerfile
make up                # local stack: API on :8080 + Mailpit inbox on :8025
```

- **Base:** `gcr.io/distroless/static-debian12:nonroot`. It holds one static binary with no shell, runs as uid 65532, and is about 18 MB.
- **Default command:** `cratecli serve`. Any CLI command also works, e.g. `docker run --rm cratecli:dev from hardwax --fast -n 3`.
- **Port:** 8080 (`CRATECLI_ADDR`).

## Configuration

Environment variables (all optional except where noted):

| Variable | What | Secret? |
|---|---|---|
| `CRATECLI_CONFIG_DIR` | folder with `config.json`, `digests.yaml`, `youtube-token.json` (image default `/config`) | |
| `CRATECLI_LIBRARY` | SQLite database file (image default `/data/library.db`) | |
| `CRATECLI_DIGESTS` | digests file (default `$CRATECLI_CONFIG_DIR/digests.yaml`) | |
| `CRATECLI_API_TOKEN` | required `Authorization: Bearer` token on `/v1` (unset = open, with a warning in the log) | yes |
| `DISCOGS_TOKEN` | Discogs personal access token (60 instead of 25 requests/min) | yes |
| `CRATECLI_SMTP_HOST` / `_PORT` / `_USERNAME` / `_PASSWORD` | mail account for digests | password |
| `CRATECLI_YOUTUBE_CLIENT_ID` / `_SECRET` | Google OAuth client | secret |
| `CRATECLI_ADDR` | listen address (default `:8080`) | |

Files:

- **`digests.yaml`:** read on every request, so edits apply without a restart.
- **`youtube-token.json`:** written by `cratecli youtube login`. The sign-in needs a browser, so do it on your Mac and copy the file in. The server rewrites it when tokens refresh, so it must be writable.

## Probes and signals

- **`GET /healthz`:** 200 while the process runs. It touches nothing else.
- **`GET /readyz`:** 200 when the database answers, 503 otherwise.
- **`GET /metrics`:** Prometheus text format: `cratecli_http_requests_total`, `cratecli_http_request_seconds_total`, `cratecli_digest_runs_total`.
- **`SIGTERM`:** stops accepting connections and finishes requests in flight for up to 30 s, then exits 0.
- **Logs:** JSON lines on stderr, one per request (probes excluded).

## State

- **All state is one SQLite file** (`CRATECLI_LIBRARY`): seen history, digs, saved records, digest runs and likes.
- **Only one process may write to it at a time.** SQLite locks the file; two servers on the same file, especially over a network filesystem, will corrupt or block it.
- **Caches** (pages and Discogs lookups) go to `$HOME/.cache/cratecli`. Losing them only costs speed.

## Triggering digests

There are two ways; pick one:

1. **`cratecli serve --schedule`:** the server checks every minute and sends what's due.
2. **From outside:** call `POST /v1/digest-runs` with `{"due": true}` as often as you like, e.g. every 15 minutes. It never sends a digest twice in a day, and returns the runs it made (empty when nothing was due).

`GET /v1/digests/{name}/runs?date=YYYY-MM-DD` says whether a digest was sent. Your Mac's fallback mode (`cratecli digest local auto`) asks exactly this before sending. Set `fallback.server` and `fallback.token` in the Mac's `digests.yaml`.

## Long requests

A dig that looks records up on Discogs can take minutes (about 2.5 s per record without a Discogs token). The server allows 15 minutes per response, so anything in front of it (ingress, proxy, client) needs a timeout at least that long. Otherwise, use `"fast": true`.

## Questions to work through for Kubernetes

These are the decisions the app leaves to you:

- **Database:** the database is a single file that only one writer may use. What does that mean for replicas, the rollout strategy, and the kind of volume you mount? When would you move to Postgres, and what would have to change in `internal/library`?
- **Scheduling:** `--schedule` inside the pod, or a scheduled job calling the API? What happens to a digest if the pod restarts at 06:59, or if two triggers overlap?
- **Secrets:** six secrets and one writable token file. Where does each live, how do they get rotated, and how does the YouTube token survive a pod being rescheduled?
- **Reaching it:** the API holds your listening history and can send email as you. Who should be able to reach `/v1`, and from where (home network, tailnet, internet)?
- **Probes:** which probe should use `/healthz` and which `/readyz`? What would a failing `/readyz` tell you?
- **Observability:** what would you alert on, given `cratecli_digest_runs_total{status="failed"}`?
- **Fallback:** when the cluster is down, your Mac sends. How does the Mac reach the cluster to ask, and what does it see if DNS works but the pod doesn't?
