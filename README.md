# cratecli

Dig for records on Discogs and take recommendations from record shops,
magazines and radio, then listen on YouTube. Not an algorithm, but a crate.

New here? Start with the [tutorial](TUTORIAL.md).

`make install` puts `cratecli` and the short alias `crate` in `/usr/local/bin`, on every Mac
user's PATH.

There are two ways in:

- **Asking**: you bring the query and Discogs answers it: `search`, `artist`, `seller`, `wants`, `release`.
- **Being recommended to**: someone else picked the records and you browse their picks: `from <source>`.

## Asking (Discogs)

```sh
crate search --style House --year 90s --random --open        # lucky dip
crate search --style House --year "late 80s" --sort want     # the classics
crate search --style "Deep House" --year 1993 --sort have --order asc   # obscurities
crate search --artist Willmo --title Kleptocracker -f json
crate artist shigeto -n 5 --open           # random tunes across their catalogue
crate artist "Theo Parrish" --year 90s
crate artist moodymann --appearances       # include remixes and features
crate seller oldschool-berlin --random     # any Discogs seller by username
crate wants <user> -f m3u | mpv --playlist=-
crate release 1 --open
```

Finding what to dig for:

```sh
crate styles house                       # look up style names (typos get suggestions)
crate styles --genre Reggae
crate styles house --year 90s            # which house styles were biggest in the 90s
crate years --style House --year 90s     # releases per year
```

`--year` takes `1995`, `1990-1999`, `90s`, `early 90s` (90-93), `mid 90s` (94-96), `late 90s` (97-99).
Search `--sort` takes `year`, `title`, `want`, `have`. These are the only sort keys the Discogs search honours.

## Being recommended to (`from`)

```sh
crate sources                              # everything available, grouped by kind
crate sources hardwax                      # a source's sections
crate from hardwax                         # Hard Wax arrivals this week
crate from hardwax essentials --random -n 5 --open
crate from oye bestsellers --style house
crate from clone -f m3u > clone.m3u
crate from pitchfork best
crate from quietus album-of-the-week
crate from nts the-trilogy-tapes           # latest episode's tracklist
crate from nts the-trilogy-tapes -e 3      # the last three episodes
crate from bbc late-junction               # Radio 3's latest tracklist
crate from bbc gilles-peterson             # 6 Music
crate from bbc b006tp52                    # any BBC show, by programme ID
crate from bandcamp --tag dub-techno       # newest; also: top, rand
crate from hardwax oye:new clone -n 12 --random   # mix sources
```

| Source | Kind | Sections (first = default) | Via |
|---|---|---|---|
| `hardwax` | shop, Berlin | this-week, last-week, essentials, reissues, techno, house, … | web page |
| `oye` | shop, Berlin | new, upcoming, used, bestsellers | web page |
| `clone` | shop, Rotterdam | new | RSS |
| `honestjons` | shop, London | latest, reggae, jazz, outernational, soul-funk, … | web page |
| `rushhour` | shop, Amsterdam | new (website), stock (Discogs) | web page + Discogs |
| `yoyaku` | shop, Paris | arrivals, forthcoming, back-in-stock, warehouse-finds, exclusive, yoyaku-records | shop API (WordPress) |
| `rubadub` | shop, Glasgow | new, recommended, back-in-stock, pre-orders, house, techno, reggae, … | shop API (Shopify) |
| `oldschool-berlin`, `decks` | shop | new | Discogs |
| `pitchfork` | press | reviews, best | RSS |
| `quietus` | press | reviews, album-of-the-week | RSS |
| `nts` | radio | `<show>` slug from nts.live/shows/…, `-e` episodes | API (unofficial) |
| `bbc` | radio | late-junction, gilles-peterson, or any programme ID; `-e` episodes | BBC programme data |
| `bandcamp` | mixed | new, top, rand + `--tag` | API (unofficial) |

Each pick keeps the shop's own details (artist, title, label, price, and its words in `note`,
e.g. Hard Wax's verdict). cratecli then looks it up on Discogs, catalogue number first, then
artist and title. When it's found you also get the videos linked on Discogs, plus styles and
year. When it isn't (brand-new arrivals often aren't on Discogs yet), the record is still shown,
with a YouTube search link built from the shop's artist and title. Useful flags:

- `--fast` skips the Discogs lookup for every pick: seconds instead of minutes, YouTube search
  links only. Good for a quick morning skim.
- `--style house` / `--year 90s` filter after the Discogs lookup, so they can't be combined with
  `--fast`. Style matching is loose and also checks the shop's own genre tags. Records not on
  Discogs only pass a style filter through those tags.
- `-n all` takes everything the source lists instead of 10. Radio digs show the whole
  tracklist unless you give `-n`.
- `-e 3` (radio) reads the last three episodes instead of the latest. Seen history means the
  same command next week shows only the new episode.
- `-f m3u` plays Bandcamp streams and shop snippets when there's no video.

**Groups** are your own mixes:

```sh
crate config group berlin hardwax oye oldschool-berlin
crate config group sunday honestjons:reggae quietus pitchfork:best
crate from @berlin -n 10 --random
```

## Seen history

`from` and `--random` digs (search, seller, wants) remember what they printed and hide it next
time, so each run shows something new. A record is recognised by artist and title, ignoring
case, accents and edition notes, or by its Discogs release, so a record seen at Hard Wax is
hidden at OYE too.

```sh
crate from hardwax oye clone --fast -n all   # everything new since last time
crate from hardwax --seen                    # include records already seen
crate last                                   # reprint the last dig, same numbers
crate seen                                   # totals, per source, recent digs
crate seen --undo                            # un-see the last dig (ran it, didn't look)
crate seen --clear --yes                     # start over
```

## Saving

Keep records from the last dig by their numbers. Records from a `--fast` dig are looked up on
Discogs as they're saved, so your crate gets the exact videos even when browsing was quick.

```sh
crate save 3 7 12
crate save 2-5 --note "for Sunday's mix"
crate saved                          # newest first; --source hardwax to filter
crate saved -f m3u | mpv --playlist=-
crate unsave 2                       # numbers as shown by `saved`
```

History and saves live in `library.db` next to the config (SQLite; override the path with
`CRATECLI_LIBRARY`).

## YouTube likes

Connect your YouTube account once and `saved` becomes everything you like, wherever you liked it:

```sh
crate youtube login           # one-time Google sign-in (needs your own OAuth client; it explains)
crate sync youtube            # new likes → saved records (origin "youtube")
crate saved --origin youtube
crate save 3                  # also likes record 3's video on YouTube (--no-like to skip)
```

Every like is kept, music or not. Titles become artist and title where possible
("Artist - Title (Official Video)", "Artist - Topic" channels). A sync reads only new likes
(1 API unit per 50); `--full` re-reads all and notices unlikes, which stay saved.

## Email digests

Themed emails of fresh records on a schedule: Monday press, Tuesday Berlin shops, and so on.
Sent records count as seen, so emails and digs never repeat each other.

```sh
crate digest init             # writes digests.yaml (schedule, recipients, sections) to edit
crate config mail             # SMTP account, e.g. iCloud with a custom domain
crate digest list
crate digest preview tuesday --open    # the email in your browser; nothing sent
crate digest send tuesday --to you@example.com
crate digest local on         # this Mac sends on schedule (launchd); off / auto / status
crate digest runs             # what went out, skipped or failed
```

Each section of a digest is a dig written like the command line:
`{ from: ["hardwax", "oye:new"], limit: all }` or `{ search: { genre: Pop, sort: want } }`.
`local auto` makes the Mac a fallback: it only sends what a server running `crate serve`
hasn't sent by the scheduled time plus a grace period.

## API (`crate serve`)

The same digs, saves, sync and digests over HTTP and JSON, for running on a server:

```sh
crate serve --schedule        # :8080; --schedule sends due digests itself
curl localhost:8080/v1/digs -d '{"from":["hardwax"],"fast":true,"limit":5}'
make up                       # Docker: API + Mailpit (a local inbox at localhost:8025)
```

The contract is in [`api/openapi.yaml`](api/openapi.yaml); what the container expects is in
[`docs/DEPLOYING.md`](docs/DEPLOYING.md).

### Not available

Dussmann, Boomkat, Juno, deejay.de, Sounds of the Universe, Bandcamp Daily, HHV and Phonica
block automated access (403 or bot challenges). OYE's staff charts load in the browser from a
private API, so they're out of reach. OYE doesn't sell on Discogs, and Hard Wax has no Discogs stock.

### Etiquette

Apart from the RSS feeds, Discogs and the product feeds that Yoyaku's and Rubadub's shop software
publishes for their own websites, sources are read from public pages. That can break when
a site changes; a test then shows which parser broke. cratecli:

- sends an honest User-Agent;
- waits 1.5 s between requests to the same host;
- caches pages for an hour;
- skips paths robots.txt disallows (e.g. Hard Wax audio);
- never uses private APIs.

## Setup

```sh
make build            # -> bin/cratecli
make install          # -> /usr/local/bin/cratecli + crate, for all users (sudo make install if not writable)
make install-go       # alternative: current user only, -> $(go env GOPATH)/bin
make uninstall
cratecli config set   # tokens -> ~/Library/Application Support/cratecli/config.json (0600)
```

Env vars `DISCOGS_TOKEN` and `YOUTUBE_API_KEY` take precedence.

| Credential | Needed? | Get it at |
|---|---|---|
| Discogs personal access token | Recommended: 60 req/min instead of 25 | discogs.com/settings/developers |
| YouTube Data API v3 key | Optional fallback, about 100 searches/day | console.cloud.google.com |

## Videos (`--video-source`)

`auto` (default) uses the videos linked on the Discogs release page, falling back to the
YouTube API (if a key is set) for releases with none. Every record also gets a free
`youtube_search_url`. The other options are `discogs`, `youtube` and `none`.

## Output (`-f`)

`table` (default): one row per record, sized to the window, coloured. In terminals that support
links (Ghostty, iTerm2, WezTerm, kitty, VS Code) titles are clickable: the title plays the video,
the label opens Discogs, the source opens the shop page. Then:

```sh
crate open 3            # play record 3 from the last dig (--shop: shop page, --discogs)
crate show 3            # every detail of record 3
crate save 3
```

`long` is the detailed multi-line view. Machine formats: `json`, `jsonl`, `csv`, `urls`, `m3u`,
`md`. Data goes to stdout and progress to stderr; colour switches off when piping, with
`NO_COLOR` set, or with `--color never`.

Other flags every dig takes: `-n 5` / `-n all` (how many), `-o`/`--open` (open each record's
best link in a browser tab, capped by `--open-max`, default 10), `--fast`, `--seen`, `-q`.

## Limits

- Discogs allows 60 req/min with a token, 25 without. Shop picks cost about 2 requests each
  (match plus release), cached for 30 days.
- Discogs search returns at most 10,000 results. Other users' inventories stop at page 100
  per sort order; `seller --random` rotates the sort order.
- Discogs has no endpoint listing styles, so the list is embedded in `internal/styles/data.go`
  (checked against live data, Nov 2026).
- Brand-new shop arrivals often aren't on Discogs yet, so they get a YouTube search link.

## Layout

```
cmd/cratecli/        main entrypoint
internal/cli/        cobra commands: flags in, core call, output out
internal/core/       the service layer every entry point shares: DigRequest, the dig pipeline
internal/discogs/    rate-limited Discogs API client
internal/crate/      Record model, Discogs sources, matching, enrichment, artist tunes
internal/sources/    curated feeds: shops, press, radio (one adapter each) + polite fetcher
internal/youtube/    YouTube search URLs, the API, Google sign-in (likes)
internal/styles/     genre/style taxonomy, style lookup, year-range parsing
internal/library/    local SQLite library: digs, seen, saved, digest runs, likes
internal/digest/     digests.yaml, building, rendering and sending digests
internal/mail/       MIME email building; SMTP and file senders
internal/ytsync/     YouTube likes → saved records
internal/api/        HTTP API handlers (crate serve)
internal/output/     table/json/csv/urls/m3u/md writers
internal/cache/      on-disk JSON cache
internal/browser/    open URLs in tabs
api/openapi.yaml     API contract
docs/                ARCHITECTURE.md (how the code fits together), DEPLOYING.md
```

New to the code, or to Go? Start with [`docs/ARCHITECTURE.md`](docs/ARCHITECTURE.md).
