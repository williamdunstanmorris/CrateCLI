// Package config loads cratecli settings from environment variables and an
// optional JSON file at $XDG_CONFIG_HOME/cratecli/config.json
// (~/Library/Application Support/cratecli/config.json on macOS).
//
// Environment variables always win over the file.
package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
)

type Config struct {
	DiscogsToken  string `json:"discogs_token,omitempty"`
	YouTubeAPIKey string `json:"youtube_api_key,omitempty"`
	// UserAgent is sent to Discogs, which requires a descriptive one.
	UserAgent string `json:"user_agent,omitempty"`
	// Groups are named mixes of sources for `cratecli from @name`,
	// e.g. "berlin": ["hardwax", "oye", "oldschool-berlin"].
	Groups map[string][]string `json:"groups,omitempty"`
	// Mail is the SMTP account digests are sent through.
	Mail Mail `json:"mail,omitzero"`
	// YouTube holds the OAuth client for reading and adding likes.
	YouTube YouTube `json:"youtube,omitzero"`
}

// Mail is an SMTP account. For iCloud (including custom domains set up in
// iCloud+) that's smtp.mail.me.com:587, your Apple Account email as the
// username and an app-specific password from account.apple.com.
type Mail struct {
	Host     string `json:"host,omitempty"`
	Port     int    `json:"port,omitempty"`
	Username string `json:"username,omitempty"`
	Password string `json:"password,omitempty"`
}

// Configured reports whether a mail server is set.
func (m Mail) Configured() bool { return m.Host != "" }

// YouTube is an OAuth client ("Desktop app") from Google Cloud Console.
type YouTube struct {
	ClientID     string `json:"client_id,omitempty"`
	ClientSecret string `json:"client_secret,omitempty"`
}

const defaultUserAgent = "cratecli/0.1 (+personal music discovery tool)"

// Path returns the location of the config file.
func Path() (string, error) {
	dir, err := Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "config.json"), nil
}

// Load reads the config file (if present) and overlays environment variables.
func Load() (Config, error) {
	var c Config
	if p, err := Path(); err == nil {
		b, err := os.ReadFile(p)
		switch {
		case err == nil:
			if err := json.Unmarshal(b, &c); err != nil {
				return c, fmt.Errorf("parse %s: %w", p, err)
			}
		case !errors.Is(err, fs.ErrNotExist):
			return c, fmt.Errorf("read %s: %w", p, err)
		}
	}
	if v := os.Getenv("DISCOGS_TOKEN"); v != "" {
		c.DiscogsToken = v
	}
	if v := os.Getenv("YOUTUBE_API_KEY"); v != "" {
		c.YouTubeAPIKey = v
	}
	if v := os.Getenv("CRATECLI_USER_AGENT"); v != "" {
		c.UserAgent = v
	}
	envOverride(&c.Mail.Host, "CRATECLI_SMTP_HOST")
	envOverride(&c.Mail.Username, "CRATECLI_SMTP_USERNAME")
	envOverride(&c.Mail.Password, "CRATECLI_SMTP_PASSWORD")
	if v := os.Getenv("CRATECLI_SMTP_PORT"); v != "" {
		port, err := strconv.Atoi(v)
		if err != nil {
			return c, fmt.Errorf("CRATECLI_SMTP_PORT: %w", err)
		}
		c.Mail.Port = port
	}
	envOverride(&c.YouTube.ClientID, "CRATECLI_YOUTUBE_CLIENT_ID")
	envOverride(&c.YouTube.ClientSecret, "CRATECLI_YOUTUBE_CLIENT_SECRET")
	if c.UserAgent == "" {
		c.UserAgent = defaultUserAgent
	}
	return c, nil
}

// Save writes the config file with owner-only permissions.
func Save(c Config) (string, error) {
	p, err := Path()
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		return "", err
	}
	if c.UserAgent == defaultUserAgent {
		c.UserAgent = ""
	}
	b, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return "", err
	}
	return p, os.WriteFile(p, append(b, '\n'), 0o600)
}

func envOverride(dst *string, name string) {
	if v := os.Getenv(name); v != "" {
		*dst = v
	}
}

// Dir is the folder holding config.json and cratecli's other files
// (library.db, digests.yaml, the YouTube token): CRATECLI_CONFIG_DIR, or
// cratecli/ in the OS config folder (~/Library/Application Support on macOS).
func Dir() (string, error) {
	if d := os.Getenv("CRATECLI_CONFIG_DIR"); d != "" {
		return d, nil
	}
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "cratecli"), nil
}
