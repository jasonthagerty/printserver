package settings

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDefaultsSaveReload(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sub", "settings.json")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := s.Get(); got.RetentionDays != 30 || got.Nextcloud.Folder != "Scans" {
		t.Errorf("defaults = %+v", got)
	}
	next := s.Get()
	next.RetentionDays = 7
	next.Nextcloud = Nextcloud{URL: " https://cloud.example.com/ ", User: " me ", Password: "app-pass", Folder: "/Docs/Scans/"}
	if err := s.Update(next); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("settings file mode %v, want 0600 (it holds a password)", info.Mode().Perm())
	}
	again, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	got := again.Get().Nextcloud
	if got.URL != "https://cloud.example.com" || got.User != "me" || got.Folder != "Docs/Scans" || got.Password != "app-pass" {
		t.Errorf("reloaded = %+v", got)
	}
}

func TestValidate(t *testing.T) {
	bad := []Settings{
		{RetentionDays: 0},
		{RetentionDays: 5000},
		{RetentionDays: 1, Nextcloud: Nextcloud{URL: "ftp://x"}},
		{RetentionDays: 1, Nextcloud: Nextcloud{URL: "https://u:p@cloud.example.com"}},
		{RetentionDays: 1, Nextcloud: Nextcloud{User: "a/b"}},
		{RetentionDays: 1, Nextcloud: Nextcloud{Folder: "Scans/../.."}},
	}
	for _, s := range bad {
		if s.Validate() == nil {
			t.Errorf("accepted %+v", s)
		}
	}
	if err := Defaults().Validate(); err != nil {
		t.Errorf("defaults invalid: %v", err)
	}
}
