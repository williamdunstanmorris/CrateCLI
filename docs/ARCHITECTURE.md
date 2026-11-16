# How cratecli is put together

A map of the code for reading it, changing it, or learning Go from it.

## The one idea

Every way into cratecli turns into the same request and goes through the same code:

```
  terminal                 email digest               HTTP API
  cratecli from hardwax    digests.yaml section       POST /v1/digs {"from":["hardwax"]}
        │                        │                          │
        │ flags → struct         │ YAML → struct            │ JSON → struct
        └──────────────┬─────────┴──────────────┬───────────┘
                       ▼                        ▼
               core.DigRequest  ──►  core.Service.Dig()
                                         │
              fetch (sources) → hide seen (library) → Discogs + videos (crate)
                                         │
                       ┌─────────────────┼──────────────────┐
                       ▼                 ▼                  ▼
                 output (table)   digest (email HTML)   api (JSON)
```

`internal/cli`, `internal/digest` and `internal/api` only translate in and out. The work happens in `internal/core` and the packages below it. A new source, filter or fix therefore reaches the CLI, the emails and the API at the same time.

## Packages, bottom to top

| Package | Does | Start reading at |
|---|---|---|
| `crate` | The `Record` type, matching records to Discogs, finding videos | `record.go`, then `enrich.go` |
| `discogs` | Rate-limited Discogs API client | `client.go` |
| `youtube` | Search URLs, the YouTube API, Google sign-in (OAuth) | `youtube.go`, `account.go`, `oauth.go` |
| `sources` | One adapter per shop, magazine or radio station; a polite HTTP fetcher | `source.go` (the `Source` interface), then any shop |
| `library` | The SQLite database: digs, seen history, saved records, digest runs, likes | `library.go` (the migrations list shows every table) |
| `core` | The service layer: `DigRequest`, the dig pipeline, pick parsing | `dig.go`, then `run.go` |
| `mail` | Building MIME emails; senders (SMTP, file) | `mail.go` |
| `digest` | `digests.yaml`, building and rendering an issue, sending, scheduling | `config.go`, `runner.go`, `render.go` |
| `ytsync` | Turning YouTube likes into saved records | `sync.go` |
| `output` | Terminal table, long view, CSV, JSON, m3u… | `table.go` |
| `api` | HTTP handlers for `cratecli serve` | `server.go` (all routes in one place) |
| `cli` | Cobra commands: flags in, `core` call, output out | `root.go`, then any command |
| `cmd/cratecli` | `main()`: three lines | |

Arrows only point down: `cli` and `api` import `core`; `core` imports `sources` and `library`; nothing imports `cli`. Go refuses import cycles, so this layering is enforced by the compiler, not just by convention.

## Following one command through the code

`cratecli from hardwax --fast -n 5`:

1. `cmd/cratecli/main.go` calls `cli.Execute()`.
2. `cli/root.go` sets up the global flags (`-n` is a `core.Limit`, which implements `pflag.Value`, so the flag parser fills it directly).
3. `cli/from.go` builds a `core.DigRequest{From: ["hardwax"]}` and calls `a.dig`.
4. `core/dig.go` `plan()` validates it: `ParsePicks` finds the `hardWax` source and its default section, and the request becomes a `Fetcher` (a function) plus `RunOptions`.
5. `core/run.go` `Run()` calls the fetcher (`sources/shops.go` `hardWax.Fetch`, which goes through `sources/fetch.go` for the 1.5 s politeness gap and the page cache), hides seen records via `library.Unseen`, takes 5, and skips Discogs because of `--fast`.
6. Back in `cli/root.go`: `emitDig` prints with `output.WriteOpts` and records the dig (`library.RecordDig`), so `cratecli save 3` knows what 3 was.

The digest (`digest/runner.go` `Send`) and the API (`api/handlers.go` `createDig`) go through steps 4 and 5 unchanged.

## Go ideas you'll meet, and where

- **Small interfaces.** `sources.Source` (two methods), `mail.Sender` (one), `ytsync.Likes` (one). Anything with those methods fits, which is how the tests swap in fakes (`digest/runner_test.go` `fakeShop`, `memSender`).
- **Struct embedding.** `api.Record` embeds `crate.Record`, so its JSON is the record's fields plus `n` and `id`.
- **Errors are values.** Wrapped with `fmt.Errorf("…: %w", err)` and checked with `errors.Is` (`library.ErrNoDigs`, `youtube.ErrNotSignedIn`).
- **`context.Context`** flows from the command or HTTP request down to every network call, so Ctrl-C or a closed connection cancels the work.
- **Concurrency.** `crate/enrich.go` looks records up 4 at a time with goroutines, a WaitGroup and a semaphore channel; `serve` uses `signal.NotifyContext` for clean shutdown.
- **Table-driven tests.** `ytsync/sync_test.go` `TestParseTitle`, `digest/digest_test.go` `TestLoadErrors`. `httptest` runs a real HTTP server in `api/api_test.go`; `mail_test.go` runs a fake SMTP server.
- **Database migrations as a list of SQL strings**, applied in order with SQLite's `PRAGMA user_version` (`library/library.go`).
- **Generics, once.** `api/handlers.go` `nonNil[T]`.

## Recipes

**Add a shop.** Write a type with `Info()` and `Fetch()` in `internal/sources` (copy `rubadub` for a JSON shop, `hardWax` for an HTML page). Add one line to `Registry` in `source.go`, and add a parser test with a saved page in `testdata/`. It then appears in `cratecli sources`, `cratecli from`, digests and `/v1/sources` with no other changes.

**Add a CLI command.** Write a `func (a *app) xxxCmd() *cobra.Command` in `internal/cli` and add it to the list in `root.go`. Put logic that isn't about flags or printing in `core` (or a package of its own), so the API can use it too.

**Add an API endpoint.** Add a route in `api/server.go` `Handler()`, a handler in `handlers.go`, a test in `api_test.go`, and the path in `api/openapi.yaml`.

**Change the database.** Append a new SQL string to `migrations` in `library/library.go`. Never edit an old one: existing databases have already run it.

## Checking your work

```sh
make test vet         # unit tests and go vet
make build && bin/cratecli from hardwax --fast -n 3
make up               # API + Mailpit in Docker (see docs/DEPLOYING.md)
```
