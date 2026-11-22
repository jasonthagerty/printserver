// Package settings stores the user-editable settings in one JSON file on
// the data drive. There is no login, so anyone on the network can change
// them; secrets are write-only through the API.
package settings

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// Settings are everything the Settings page edits.
type Settings struct {
	RetentionDays int       `json:"retention_days"`
	Nextcloud     Nextcloud `json:"nextcloud"`
}

// Nextcloud is a WebDAV destination for scans.
type Nextcloud struct {
	URL      string `json:"url"`      // https://cloud.example.com
	User     string `json:"user"`     // login name
	Password string `json:"password"` // an app password, never returned by the API
	Folder   string `json:"folder"`   // e.g. Scans
}

// Configured reports whether every field needed to upload is set.
func (n Nextcloud) Configured() bool {
	return n.URL != "" && n.User != "" && n.Password != ""
}

// Defaults are used for anything the file does not set.
func Defaults() Settings {
	return Settings{RetentionDays: 30, Nextcloud: Nextcloud{Folder: "Scans"}}
}

// Validate checks values a user typed.
func (s Settings) Validate() error {
	if s.RetentionDays < 1 || s.RetentionDays > 3650 {
		return errors.New("retention must be between 1 and 3650 days")
	}
	n := s.Nextcloud
	if n.URL != "" {
		u, err := url.Parse(n.URL)
		if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
			return errors.New("Nextcloud URL must be an http or https address")
		}
		if u.User != nil {
			return errors.New("put the user and password in their own fields, not the URL")
		}
		if u.RawQuery != "" || u.Fragment != "" {
			return errors.New("the Nextcloud URL must be just the server address, like https://cloud.example.com")
		}
	}
	if strings.Contains(n.User, "/") {
		return errors.New("Nextcloud user cannot contain /")
	}
	for _, part := range strings.Split(strings.Trim(n.Folder, "/"), "/") {
		if part == ".." || part == "." {
			return errors.New("the Nextcloud folder cannot contain . or .. parts")
		}
	}
	return nil
}

// Store loads and saves the settings file.
type Store struct {
	path string
	mu   sync.Mutex
	cur  Settings
}

// Open reads path, or starts from defaults if it does not exist.
func Open(path string) (*Store, error) {
	s := &Store{path: path, cur: Defaults()}
	b, err := os.ReadFile(path)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return s, nil
	case err != nil:
		return nil, err
	}
	if err := json.Unmarshal(b, &s.cur); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	if s.cur.RetentionDays == 0 {
		s.cur.RetentionDays = Defaults().RetentionDays
	}
	return s, nil
}

// Get returns the current settings, secrets included.
func (s *Store) Get() Settings {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.cur
}

// Update validates and saves next.
func (s *Store) Update(next Settings) error {
	next.Nextcloud.URL = strings.TrimRight(strings.TrimSpace(next.Nextcloud.URL), "/")
	next.Nextcloud.User = strings.TrimSpace(next.Nextcloud.User)
	next.Nextcloud.Folder = strings.Trim(strings.TrimSpace(next.Nextcloud.Folder), "/")
	if err := next.Validate(); err != nil {
		return err
	}
	b, err := json.MarshalIndent(next, "", "  ")
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := writeAtomic(s.path, b); err != nil {
		return err
	}
	s.cur = next
	return nil
}

func writeAtomic(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".settings-*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	_, werr := tmp.Write(data)
	if werr == nil {
		werr = tmp.Sync()
	}
	if cerr := tmp.Close(); werr == nil {
		werr = cerr
	}
	if werr == nil {
		werr = os.Chmod(name, 0o600)
	}
	if werr != nil {
		os.Remove(name)
		return werr
	}
	return os.Rename(name, path)
}
