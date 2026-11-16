package digest

// Example is the digests.yaml written by `cratecli digest init`.
const Example = `# cratecli email digests. Edit, then check with: cratecli digest list
# Each section is a dig, written like the command line:
#   from: [hardwax, "oye:new", "@berlin"]   = cratecli from hardwax oye:new @berlin
#   search: { genre: Pop, sort: want }      = cratecli search --genre Pop --sort want
# Digests never repeat records: what's been sent counts as seen.

timezone: Europe/Berlin

# The sender: an address your mail account may send from
# (set the account up with: cratecli config mail).
from: "Crate <music@example.com>"

# Named recipients; digests refer to them by name. Each gets their own email.
recipients:
  me: you@example.com

digests:
  - name: monday
    title: Press
    days: [mon]
    at: "07:00"
    to: [me]
    sections:
      - title: Pitchfork Best New Music
        dig: { from: ["pitchfork:best"], limit: all }
      - title: The Quietus
        dig: { from: ["quietus:album-of-the-week", "quietus:reviews"], limit: 15 }

  - name: tuesday
    title: Berlin shops
    days: [tue]
    at: "07:00"
    to: [me]
    sections:
      - title: Hard Wax
        dig: { from: [hardwax], limit: all }
      - title: OYE
        dig: { from: ["oye:new"], limit: 30 }

  - name: wednesday
    title: Avant-garde
    days: [wed]
    at: "07:00"
    to: [me]
    sections:
      - title: Late Junction
        dig: { from: ["bbc:late-junction"], limit: all }
      - title: Honest Jon's
        dig: { from: ["honestjons:outernational"], limit: 15 }

  - name: thursday
    title: DJs and radio
    days: [thu]
    at: "07:00"
    to: [me]
    sections:
      - title: Gilles Peterson
        dig: { from: ["bbc:gilles-peterson"], limit: all }
      - title: The Trilogy Tapes on NTS
        dig: { from: ["nts:the-trilogy-tapes"], limit: all }

  - name: friday
    title: Pop
    days: [fri]
    at: "07:00"
    to: [me]
    sections:
      - title: Most-wanted new pop on Discogs
        dig: { search: { genre: Pop, year: "2025-2026", sort: want }, limit: 25 }

# When a server normally sends the digests, this Mac can stand in for it:
# fallback:
#   server: http://crate.example:8080
#   grace: 15m
#   token: <the server's CRATECLI_API_TOKEN>
`
