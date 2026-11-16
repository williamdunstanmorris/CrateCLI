package youtube

// Signing in with Google, the "desktop app" way: cratecli opens Google's
// consent page in your browser and listens on a random local port for
// Google to redirect back with a one-time code (RFC 8252, with PKCE).
// The resulting refresh token is saved to a file only you can read.

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	"golang.org/x/oauth2"
)

// Scope lets cratecli read your likes and like videos. ("youtube.readonly"
// would be enough for syncing, but not for liking.)
const Scope = "https://www.googleapis.com/auth/youtube"

// googleEndpoint is Google's OAuth 2.0 endpoint (the same values as
// golang.org/x/oauth2/google, without pulling in its cloud dependencies).
var googleEndpoint = oauth2.Endpoint{
	AuthURL:   "https://accounts.google.com/o/oauth2/auth",
	TokenURL:  "https://oauth2.googleapis.com/token",
	AuthStyle: oauth2.AuthStyleInParams,
}

// OAuthConfig builds the OAuth client from your Google Cloud credentials.
func OAuthConfig(clientID, clientSecret string) *oauth2.Config {
	return &oauth2.Config{
		ClientID:     clientID,
		ClientSecret: clientSecret,
		Endpoint:     googleEndpoint,
		Scopes:       []string{Scope},
	}
}

// Login runs the browser sign-in and returns a token with a refresh token.
// open is called with the URL to visit (it should open a browser).
func Login(ctx context.Context, cfg *oauth2.Config, open func(string) error) (*oauth2.Token, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	defer ln.Close()
	c := *cfg
	c.RedirectURL = fmt.Sprintf("http://%s/callback", ln.Addr())

	state := randomHex(16)
	verifier := oauth2.GenerateVerifier()
	authURL := c.AuthCodeURL(state, oauth2.AccessTypeOffline, oauth2.ApprovalForce, oauth2.S256ChallengeOption(verifier))

	type result struct {
		code string
		err  error
	}
	done := make(chan result, 1)
	var once sync.Once
	srv := &http.Server{
		ReadHeaderTimeout: 10 * time.Second,
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/callback" {
				http.NotFound(w, r)
				return
			}
			q := r.URL.Query()
			var res result
			switch {
			case q.Get("state") != state:
				res.err = errors.New("sign-in failed: state mismatch")
			case q.Get("error") != "":
				res.err = fmt.Errorf("sign-in refused: %s", q.Get("error"))
			default:
				res.code = q.Get("code")
			}
			if res.err != nil {
				http.Error(w, res.err.Error(), http.StatusBadRequest)
			} else {
				fmt.Fprintln(w, "Signed in to YouTube. You can close this tab and go back to the terminal.")
			}
			once.Do(func() { done <- res })
		}),
	}
	go srv.Serve(ln)
	defer srv.Close()

	if err := open(authURL); err != nil {
		return nil, err
	}
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-time.After(5 * time.Minute):
		return nil, errors.New("sign-in timed out after 5 minutes")
	case res := <-done:
		if res.err != nil {
			return nil, res.err
		}
		return c.Exchange(ctx, res.code, oauth2.VerifierOption(verifier))
	}
}

func randomHex(n int) string {
	b := make([]byte, n)
	rand.Read(b)
	return hex.EncodeToString(b)
}

// TokenFile stores the token on disk (0600) and saves refreshed tokens.
type TokenFile struct{ Path string }

// DefaultTokenFile is youtube-token.json in dir (the config folder).
func DefaultTokenFile(dir string) TokenFile {
	return TokenFile{Path: filepath.Join(dir, "youtube-token.json")}
}

// ErrNotSignedIn means there's no saved token.
var ErrNotSignedIn = errors.New("not signed in to YouTube; run `cratecli youtube login`")

// Load reads the saved token.
func (f TokenFile) Load() (*oauth2.Token, error) {
	b, err := os.ReadFile(f.Path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, ErrNotSignedIn
	}
	if err != nil {
		return nil, err
	}
	var t oauth2.Token
	if err := json.Unmarshal(b, &t); err != nil {
		return nil, fmt.Errorf("%s: %w", f.Path, err)
	}
	return &t, nil
}

// Save writes the token, readable only by you.
func (f TokenFile) Save(t *oauth2.Token) error {
	if err := os.MkdirAll(filepath.Dir(f.Path), 0o700); err != nil {
		return err
	}
	b, err := json.MarshalIndent(t, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(f.Path, b, 0o600)
}

// Delete signs out locally.
func (f TokenFile) Delete() error {
	err := os.Remove(f.Path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	return err
}

// TokenSource refreshes access tokens as needed and saves new ones.
func (f TokenFile) TokenSource(ctx context.Context, cfg *oauth2.Config) (oauth2.TokenSource, error) {
	t, err := f.Load()
	if err != nil {
		return nil, err
	}
	return &savingSource{file: f, last: t, src: cfg.TokenSource(ctx, t)}, nil
}

type savingSource struct {
	mu   sync.Mutex
	file TokenFile
	last *oauth2.Token
	src  oauth2.TokenSource
}

func (s *savingSource) Token() (*oauth2.Token, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	t, err := s.src.Token()
	if err != nil {
		return nil, err
	}
	if t.AccessToken != s.last.AccessToken {
		s.last = t
		_ = s.file.Save(t)
	}
	return t, nil
}
