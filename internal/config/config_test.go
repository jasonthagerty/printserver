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
