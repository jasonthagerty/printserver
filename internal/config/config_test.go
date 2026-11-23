package config

import (
	"slices"
	"testing"
)

func TestFromEnv(t *testing.T) {
	t.Setenv("PRINTSERVER_DEVICE", "usb")
	t.Setenv("PRINTSERVER_ALLOWED_HOSTS", " print.example.com, ,Other.example ")
	t.Setenv("PRINTSERVER_DATA", "relative/data")
	c, err := FromEnv()
	if err != nil {
		t.Fatal(err)
	}
	if c.Device != "usb" || c.Hostname != "printserver" || c.Queue != "MG3620" {
		t.Errorf("config = %+v", c)
	}
	if !slices.Equal(c.ExtraHosts, []string{"print.example.com", "Other.example"}) {
		t.Errorf("extra hosts = %q", c.ExtraHosts)
	}
	if c.DataDir[0] != '/' {
		t.Errorf("data dir %q is not absolute", c.DataDir)
	}
}

func TestBadDevice(t *testing.T) {
	t.Setenv("PRINTSERVER_DEVICE", "network")
	if _, err := FromEnv(); err == nil {
		t.Fatal("accepted an unknown device mode")
	}
}

func TestPublicURL(t *testing.T) {
	t.Setenv("PRINTSERVER_PUBLIC_URL", "http://Print.Example.com")
	c, err := FromEnv()
	if err != nil {
		t.Fatal(err)
	}
	if c.PublicURL != "http://Print.Example.com/" || !slices.Contains(c.ExtraHosts, "print.example.com") {
		t.Errorf("url %q, hosts %q", c.PublicURL, c.ExtraHosts)
	}
	t.Setenv("PRINTSERVER_PUBLIC_URL", "ftp://x")
	if _, err := FromEnv(); err == nil {
		t.Error("accepted an ftp URL")
	}
}

func TestPublicURLDefault(t *testing.T) {
	c, err := FromEnv()
	if err != nil {
		t.Fatal(err)
	}
	if c.PublicURL != "http://printserver.local/" || len(c.ExtraHosts) != 0 {
		t.Errorf("url %q, hosts %q", c.PublicURL, c.ExtraHosts)
	}
}
