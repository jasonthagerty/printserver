// Package avahi publishes mDNS services by writing Avahi's static service
// files. The printer's and scanner's records are written by the Go server,
// not by CUPS or AirSane, so they can carry this server's own URL and
// names (see internal/airprint and internal/airscan).
package avahi

import (
	"bytes"
	"context"
	"encoding/xml"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

// Service is one <service> in a service group.
type Service struct {
	Type     string   // e.g. _ipp._tcp
	Subtypes []string // e.g. _universal._sub._ipp._tcp
	Port     int
	TXT      []string // key=value
}

// File renders a service group as Avahi's XML service file.
func File(name string, services ...Service) []byte {
	var b bytes.Buffer
	b.WriteString("<?xml version=\"1.0\" standalone='no'?>\n<!DOCTYPE service-group SYSTEM \"avahi-service.dtd\">\n<service-group>\n")
	fmt.Fprintf(&b, "  <name>%s</name>\n", esc(name))
	for _, s := range services {
		fmt.Fprintf(&b, "  <service>\n    <type>%s</type>\n", esc(s.Type))
		for _, st := range s.Subtypes {
			fmt.Fprintf(&b, "    <subtype>%s</subtype>\n", esc(st))
		}
		fmt.Fprintf(&b, "    <port>%d</port>\n", s.Port)
		for _, t := range s.TXT {
			fmt.Fprintf(&b, "    <txt-record>%s</txt-record>\n", esc(t))
		}
		b.WriteString("  </service>\n")
	}
	b.WriteString("</service-group>\n")
	return b.Bytes()
}

// Dir is Avahi's services directory.
type Dir struct {
	Path string
	// Reload makes Avahi reread its service files. Defaults to
	// `avahi-daemon --reload`.
	Reload func(ctx context.Context) error
}

// Set writes content as name in the directory, or removes the file when
// content is nil, and reloads Avahi if anything changed. It reports whether
// the file changed.
func (d Dir) Set(ctx context.Context, name string, content []byte) (bool, error) {
	path := filepath.Join(d.Path, name)
	have, _ := os.ReadFile(path)
	if bytes.Equal(have, content) {
		return false, nil
	}
	if content == nil {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return false, err
		}
	} else {
		tmp := path + ".tmp"
		if err := os.WriteFile(tmp, content, 0o644); err != nil {
			return false, err
		}
		if err := os.Rename(tmp, path); err != nil {
			return false, err
		}
	}
	reload := d.Reload
	if reload == nil {
		reload = func(ctx context.Context) error {
			return exec.CommandContext(ctx, "avahi-daemon", "--reload").Run()
		}
	}
	return true, reload(ctx)
}

func esc(s string) string {
	var b bytes.Buffer
	_ = xml.EscapeText(&b, []byte(s))
	return b.String()
}
