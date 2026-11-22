// Package escl is a client for the eSCL (AirScan) protocol, used to drive
// the scanner through AirSane. iPhones and Macs use the same server, so
// AirSane alone owns the scanner and the two never collide.
package escl

import (
	"bytes"
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"
)

// Client talks to one eSCL scanner at base, e.g. http://127.0.0.1:8090/eSCL.
type Client struct {
	base string
	http *http.Client
}

// New returns a client. hc may be nil.
func New(base string, hc *http.Client) *Client {
	if hc == nil {
		hc = &http.Client{}
	}
	return &Client{base: strings.TrimSuffix(base, "/"), http: hc}
}

// ErrBusy means the scanner is already scanning for another client.
var ErrBusy = errors.New("escl: scanner busy")

// ErrUnavailable means there is no scanner to talk to: AirSane found none,
// or it is not running.
var ErrUnavailable = errors.New("escl: scanner unavailable")

// Capabilities is the subset of ScannerCapabilities the UI needs.
type Capabilities struct {
	MakeAndModel string
	UUID         string
	ColorModes   []string // RGB24, Grayscale8, BlackAndWhite1
	Formats      []string // MIME types
	Resolutions  []int    // dpi, ascending
	MaxWidth     int      // 1/300 inch
	MaxHeight    int      // 1/300 inch
}

type capsXML struct {
	MakeAndModel string `xml:"MakeAndModel"`
	UUID         string `xml:"UUID"`
	Platen       struct {
		InputCaps struct {
			MaxWidth  int `xml:"MaxWidth"`
			MaxHeight int `xml:"MaxHeight"`
			Profiles  []struct {
				ColorModes []string `xml:"ColorModes>ColorMode"`
				Formats    []string `xml:"DocumentFormats>DocumentFormat"`
				FormatsExt []string `xml:"DocumentFormats>DocumentFormatExt"`
				Res        []struct {
					X int `xml:"XResolution"`
				} `xml:"SupportedResolutions>DiscreteResolutions>DiscreteResolution"`
			} `xml:"SettingProfiles>SettingProfile"`
		} `xml:"PlatenInputCaps"`
	} `xml:"Platen"`
}

// Capabilities fetches what the platen supports.
func (c *Client) Capabilities(ctx context.Context) (Capabilities, error) {
	body, err := c.get(ctx, "/ScannerCapabilities")
	if err != nil {
		return Capabilities{}, err
	}
	var x capsXML
	if err := xml.Unmarshal(body, &x); err != nil {
		return Capabilities{}, fmt.Errorf("escl: capabilities: %w", err)
	}
	in := x.Platen.InputCaps
	caps := Capabilities{MakeAndModel: strings.TrimSpace(x.MakeAndModel), UUID: strings.TrimSpace(x.UUID), MaxWidth: in.MaxWidth, MaxHeight: in.MaxHeight}
	for _, p := range in.Profiles {
		caps.ColorModes = appendNew(caps.ColorModes, p.ColorModes...)
		caps.Formats = appendNew(caps.Formats, p.Formats...)
		caps.Formats = appendNew(caps.Formats, p.FormatsExt...)
		for _, r := range p.Res {
			if r.X > 0 && !slices.Contains(caps.Resolutions, r.X) {
				caps.Resolutions = append(caps.Resolutions, r.X)
			}
		}
	}
	slices.Sort(caps.Resolutions)
	if caps.MaxWidth <= 0 || caps.MaxHeight <= 0 {
		return Capabilities{}, errors.New("escl: capabilities have no platen size")
	}
	return caps, nil
}

// Status is the scanner's state: Idle, Processing, Testing, Stopped, Down.
func (c *Client) Status(ctx context.Context) (string, error) {
	body, err := c.get(ctx, "/ScannerStatus")
	if err != nil {
		return "", err
	}
	var x struct {
		State string `xml:"State"`
	}
	if err := xml.Unmarshal(body, &x); err != nil {
		return "", fmt.Errorf("escl: status: %w", err)
	}
	return x.State, nil
}

// Settings select one platen scan of the full glass.
type Settings struct {
	Resolution int    // dpi
	ColorMode  string // RGB24 or Grayscale8
	Format     string // image/jpeg, application/pdf, image/png
	Width      int    // 1/300 inch; from Capabilities.MaxWidth
	Height     int    // 1/300 inch; from Capabilities.MaxHeight
}

const settingsTmpl = `<?xml version="1.0" encoding="UTF-8"?>
<scan:ScanSettings xmlns:scan="http://schemas.hp.com/imaging/escl/2011/05/03" xmlns:pwg="http://www.pwg.org/schemas/2010/12/sm">
  <pwg:Version>2.0</pwg:Version>
  <pwg:ScanRegions>
    <pwg:ScanRegion>
      <pwg:ContentRegionUnits>escl:ThreeHundredthsOfInches</pwg:ContentRegionUnits>
      <pwg:XOffset>0</pwg:XOffset>
      <pwg:YOffset>0</pwg:YOffset>
      <pwg:Width>%d</pwg:Width>
      <pwg:Height>%d</pwg:Height>
    </pwg:ScanRegion>
  </pwg:ScanRegions>
  <pwg:InputSource>Platen</pwg:InputSource>
  <scan:ColorMode>%s</scan:ColorMode>
  <scan:XResolution>%d</scan:XResolution>
  <scan:YResolution>%d</scan:YResolution>
  <pwg:DocumentFormat>%s</pwg:DocumentFormat>
  <scan:DocumentFormatExt>%s</scan:DocumentFormatExt>
</scan:ScanSettings>
`

// Scan runs one scan and returns the image. The caller must close it.
func (c *Client) Scan(ctx context.Context, s Settings) (io.ReadCloser, string, error) {
	if s.Width <= 0 || s.Height <= 0 || s.Resolution <= 0 {
		return nil, "", errors.New("escl: scan settings need a region and resolution")
	}
	var esc bytes.Buffer
	for _, v := range []string{s.ColorMode, s.Format} {
		if strings.ContainsAny(v, "<>&\"'") {
			return nil, "", fmt.Errorf("escl: invalid setting %q", v)
		}
	}
	fmt.Fprintf(&esc, settingsTmpl, s.Width, s.Height, s.ColorMode, s.Resolution, s.Resolution, s.Format, s.Format)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.base+"/ScanJobs", &esc)
	if err != nil {
		return nil, "", err
	}
	req.Header.Set("Content-Type", "text/xml")
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, "", fmt.Errorf("%w: %w", ErrUnavailable, err)
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	resp.Body.Close()
	switch resp.StatusCode {
	case http.StatusCreated:
	case http.StatusServiceUnavailable, http.StatusConflict:
		return nil, "", ErrBusy
	case http.StatusNotFound:
		return nil, "", ErrUnavailable
	default:
		return nil, "", fmt.Errorf("escl: create scan job: HTTP %s", resp.Status)
	}
	job, err := c.resolve(resp.Header.Get("Location"))
	if err != nil {
		return nil, "", err
	}
	ok := false
	defer func() {
		if !ok {
			c.cancel(job)
		}
	}()

	// NextDocument blocks until the scan is done. A busy device may answer
	// 503 at first, so retry briefly.
	for attempt := 0; ; attempt++ {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, job+"/NextDocument", nil)
		if err != nil {
			return nil, "", err
		}
		resp, err := c.http.Do(req)
		if err != nil {
			return nil, "", fmt.Errorf("escl: next document: %w", err)
		}
		if resp.StatusCode == http.StatusOK {
			ct := resp.Header.Get("Content-Type")
			if ct == "" {
				ct = s.Format
			}
			ok = true
			return resp.Body, ct, nil
		}
		_, _ = io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
		if resp.StatusCode != http.StatusServiceUnavailable || attempt >= 10 {
			return nil, "", fmt.Errorf("escl: next document: HTTP %s", resp.Status)
		}
		select {
		case <-ctx.Done():
			return nil, "", ctx.Err()
		case <-time.After(time.Second):
		}
	}
}

// cancel deletes a scan job that will not be read, so AirSane frees the
// scanner now rather than after its job timeout. It must outlive the
// caller's context, which may be what was cancelled.
func (c *Client) cancel(job string) {
	ctx, done := context.WithTimeout(context.Background(), 5*time.Second)
	defer done()
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, job, nil)
	if err != nil {
		return
	}
	if resp, err := c.http.Do(req); err == nil {
		_, _ = io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
	}
}

func (c *Client) resolve(loc string) (string, error) {
	if loc == "" {
		return "", errors.New("escl: scan job created without a Location")
	}
	base, err := url.Parse(c.base)
	if err != nil {
		return "", err
	}
	u, err := base.Parse(loc)
	if err != nil {
		return "", fmt.Errorf("escl: bad job location %q: %w", loc, err)
	}
	// Only follow jobs on the same server.
	if u.Host != base.Host {
		return "", fmt.Errorf("escl: job location %q is on another host", loc)
	}
	return strings.TrimSuffix(u.String(), "/"), nil
}

func (c *Client) get(ctx context.Context, path string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base+path, nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrUnavailable, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return nil, ErrUnavailable
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("escl: GET %s: HTTP %s", path, resp.Status)
	}
	return io.ReadAll(io.LimitReader(resp.Body, 1<<20))
}

func appendNew(dst []string, vals ...string) []string {
	for _, v := range vals {
		v = strings.TrimSpace(v)
		if v != "" && !slices.Contains(dst, v) {
			dst = append(dst, v)
		}
	}
	return dst
}
