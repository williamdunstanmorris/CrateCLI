# cratecli tutorial

A walkthrough from install to a daily ten-minute dig. About 15 minutes the first time.
Every command here is safe to run: digging only reads from shops and Discogs, and the only
thing written is your own history file.

## 1. Install

```sh
cd CrateCLI
make install        # builds, then copies cratecli and the alias crate into /usr/local/bin
crate --help
```

`/usr/local/bin` is on every Mac user's PATH, so other accounts on the laptop can run `crate`
too. Each account keeps its own token, history and saves in its own
`~/Library/Application Support/cratecli/`. After changing the code, run `make install` again.
If it says permission denied, use `sudo make install`.

## 2. Add a Discogs token (recommended, 2 minutes)

Without one, everything works but Discogs lookups run at 25 a minute instead of 60.

1. Go to discogs.com → Settings → Developers → **Generate new token**.
2. Run `crate config set` and paste it when asked. Press Enter to skip the YouTube key.
3. Check it with `crate config show` (the token is shown masked).

The token only lets cratecli read Discogs as you. It's stored in
`~/Library/Application Support/cratecli/config.json`, readable only by your user.

## 3. Reading the output

```sh
crate from hardwax yoyaku rubadub --fast -n 9
```

You get one row per record, sized to your window:

```
#  ARTIST              TITLE            LABEL               PRICE  FROM     NOTE                        LISTEN
1  Chez Damier         Can You Feel It  Balance            €18.00  hardwax  Reissue of early 1990s D…   ▶ 3 videos
2  Aja & Mami Konishi  Shimo Naps       Chapelle XIV Music €19.50  yoyaku   Born from an encounter i…   ⌕ search
crate open 3 · crate show 3 · crate save 3 · -f long for everything
```

- **Click the title** to play it (Ghostty and most modern terminals support clickable text):
  the first video, or a YouTube search if there's none. The label opens Discogs; the source
  name opens the shop's page.
- **LISTEN** says what you'll get: `▶` the exact videos linked on Discogs, `♪` a stream or shop
  snippet, `⌕` a YouTube search for artist and title (records not on Discogs yet, or `--fast`).
- **NOTE** is the shop's own words. It appears when the window is wide (140+ columns).
- **The numbers** are how you refer to records next:

```sh
crate open 1        # play it (--shop for the shop page, --discogs)
crate show 1        # every detail: styles, all video links, full note
crate save 1
```

`-f long` prints every record in full, the way `show` does. When you pipe or redirect output,
colours and links switch off automatically.

## 4. The flags you'll use most

| Flag | Means | Example |
|---|---|---|
| `-n 5` | how many records (default 10) | `crate from oye -n 5` |
| `-n all` | everything the source lists | `crate from hardwax -n all` |
| `--fast` | skip Discogs: seconds instead of minutes, search links only | `crate from hardwax --fast` |
| `-o`, `--open` | open each record in a browser tab (max 10 unless `--open-max`) | `crate from clone -n 3 -o` |
| `-r`, `--random` | shuffle instead of newest first | `crate from hardwax essentials -r` |
| `--seen` | include records you've already seen | `crate from hardwax --seen` |
| `-f` | output format: `table`, `long`, `json`, `csv`, `urls`, `m3u`, `md` | `crate from oye -f csv > oye.csv` |

When is `--fast` worth it? Discogs lookups cost about two requests per record. Ten records take
seconds; `-n all` on three shops (100+ records) takes several minutes the first time, though
lookups are cached for 30 days afterwards. For skimming, use `--fast` and open what catches
your eye.

## 5. Explore the sources

```sh
crate sources                 # every shop, magazine and radio show, with sections
crate sources hardwax         # Hard Wax's sections: this-week, essentials, techno, …
crate from hardwax essentials -n 5
crate from oye bestsellers --style house
crate from honestjons reggae
crate from yoyaku warehouse-finds
crate from rubadub recommended
crate from pitchfork best
crate from nts the-trilogy-tapes    # the show slug from nts.live/shows/<slug>
crate from bandcamp --tag dub-techno
```

Radio shows give you their tracklists, one row per track, latest episode first:

```sh
crate from bbc late-junction          # Radio 3
crate from bbc gilles-peterson        # 6 Music
crate from nts the-trilogy-tapes      # slug from nts.live/shows/<slug>
crate from nts the-trilogy-tapes -e 3 # the last three episodes
```

Any other BBC show works by its programme ID, the code at the end of its
bbc.co.uk/programmes/… or BBC Sounds address: `crate from bbc b006tp52`. A radio group is
handy: `crate config group radio bbc:late-junction bbc:gilles-peterson nts:the-trilogy-tapes`,
then `crate from @radio` each week shows just the new episodes.

Mix sources in one dig; they alternate:

```sh
crate from hardwax oye:new clone -n 12
```

Save a mix you like as a group:

```sh
crate config group berlin hardwax oye oldschool-berlin
crate from @berlin
```

## 6. Seen history: only what's new

`from` (and `--random` digs) remember everything they printed and hide it next time:

```sh
crate from hardwax --fast -n all    # all 32 of this week's records
crate from hardwax --fast -n all    # "nothing new: you've seen all 32"
```

A record seen at Hard Wax is hidden at OYE too. Useful commands:

```sh
crate last           # print the last dig again, same numbers, no fetching
crate last -o        # …and open it in tabs
crate seen           # how much you've seen, per source, and recent digs
crate seen --undo    # ran a dig but didn't really look? un-see it
```

## 7. Saving what you like

Save records from the last dig by their numbers:

```sh
crate save 3 7 12
crate save 2-5 --note "for Sunday's mix"
crate saved                     # your crate, newest first
crate saved -o                  # open them all in tabs
crate unsave 2                  # numbers as shown by `saved`
```

If the dig was `--fast`, saving looks the records up on Discogs, so your saved crate has the
exact videos where they exist. Only the few records you save cost lookups.

## 8. The morning routine

```sh
crate from @berlin clone yoyaku --fast -n all   # 1. everything new, numbered, in seconds
                                                # 2. click what catches your eye
crate save 4 11 23                              # 3. keep the good ones
crate last                                      #    (forgot a number? reprint the dig)
```

Tomorrow the same command shows only what arrived since.

## 8b. Let it come to you: email digests

Instead of digging every morning, get one themed email a day:

```sh
crate digest init           # writes digests.yaml; open it and set from: and recipients:
crate config mail           # your mail account (see below)
crate digest list           # check the schedule
crate digest preview monday --open   # see the email in your browser (nothing is sent)
crate digest send monday --to you@example.com   # a real test email
crate digest local on       # from now on your Mac sends them on schedule
```

**Mail through iCloud with your own domain.** If your domain's addresses are set up in iCloud+,
use `smtp.mail.me.com`, port `587`, your Apple Account email as the username, and an
**app-specific password** (account.apple.com → Sign-In and Security → App-Specific Passwords),
never your real one. Then put any of your domain's addresses in `from:` in digests.yaml.

Every record in an email counts as seen, so the next dig or digest won't repeat it. Each email
is numbered like a dig, so `crate save 12` saves record 12 from it, as long as you haven't dug since.
`crate digest local off` stops sending; `crate digest runs` shows what went out.

## 8c. Your YouTube likes as your crate

```sh
crate youtube login          # explains the one-time Google setup, then opens a sign-in
crate sync youtube           # pulls your likes into `crate saved`
crate saved --origin youtube
```

From then on, `crate save 3` also likes the video on YouTube, and `crate sync youtube` brings in
what you liked while clicking around YouTube.

## 9. Asking Discogs directly

When you know what you're after rather than browsing someone's picks:

```sh
crate styles house                              # find exact style names
crate years --style "Deep House"                # when was it big?
crate search --style "Deep House" --year 1993 --random -o     # lucky dip
crate search --style House --year "late 80s" --sort want      # the classics
crate artist "Theo Parrish" -n 5 -o            # random tunes across a catalogue
crate seller oldschool-berlin --random          # any Discogs shop
```

`--random` searches also use seen history, so repeated lucky dips don't repeat.

## 10. Listening without a browser

With `mpv` and `yt-dlp` installed (`brew install mpv yt-dlp`):

```sh
crate from clone -f m3u | mpv --playlist=-
crate last -f m3u > today.m3u
crate saved -f m3u | mpv --playlist=-
```

## If something goes wrong

- **"no DISCOGS_TOKEN set"**: harmless; see step 2.
- **A shop returns nothing or errors**: its website probably changed. Other sources keep working.
- **Lookups feel slow**: add a token, use `--fast`, or a smaller `-n`.
- **You want to start fresh**: `crate seen --clear --yes`.
