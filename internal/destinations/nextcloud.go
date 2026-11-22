// Package destinations sends finished scans somewhere other than the
// browser. Nextcloud is reached with WebDAV and an app password.
package destinations

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/jasonthagerty/printserver/internal/settings"
)

// Nextcloud uploads files to a user's Nextcloud.
type Nextcloud struct {
	http *http.Client
}

// NewNextcloud returns an uploader. hc may be nil.
func NewNextcloud(hc *http.Client) *Nextcloud {
	if hc == nil {
		hc = &http.Client{Timeout: 5 * time.Minute}
	}
	return &Nextcloud{http: hc}
}

// ErrNotConfigured means the URL, user or app password is missing.
var ErrNotConfigured = errors.New("Nextcloud is not set up: add its address, user and app password in Settings")

func davBase(c settings.Nextcloud) string {
	return c.URL + "/remote.php/dav/files/" + url.PathEscape(c.User)
}

func folderPath(folder string) string {
	var parts []string
	for _, p := range strings.Split(strings.Trim(folder, "/"), "/") {
		if p != "" {
			parts = append(parts, url.PathEscape(p))
		}
	}
	return strings.Join(parts, "/")
}

// Test checks that the credentials work and the folder can be created.
func (n *Nextcloud) Test(ctx context.Context, c settings.Nextcloud) error {
	if !c.Configured() {
		return ErrNotConfigured
	}
	resp, err := n.request(ctx, c, "PROPFIND", davBase(c)+"/", nil, map[string]string{"Depth": "0"})
	if err != nil {
		return err
	}
	resp.Body.Close()
	if err := statusErr(resp, http.StatusMultiStatus); err != nil {
		return err
	}
	return n.ensureFolder(ctx, c)
}

// Upload stores body as name in the configured folder and returns its path
// in Nextcloud. An existing file with the same name gets a numbered name
// rather than being overwritten.
func (n *Nextcloud) Upload(ctx context.Context, c settings.Nextcloud, name string, body io.ReadSeeker, contentType string) (string, error) {
	if !c.Configured() {
		return "", ErrNotConfigured
	}
	if err := n.ensureFolder(ctx, c); err != nil {
		return "", err
	}
	// Send a length rather than chunked encoding, which some Nextcloud
	// front ends refuse for WebDAV uploads.
	size, err := body.Seek(0, io.SeekEnd)
	if err != nil {
		return "", err
	}
	folder := folderPath(c.Folder)
	base, ext := splitExt(name)
	for i := 0; i < 100; i++ {
		candidate := name
		if i > 0 {
			candidate = fmt.Sprintf("%s (%d)%s", base, i, ext)
		}
		rel := url.PathEscape(candidate)
		if folder != "" {
			rel = folder + "/" + rel
		}
		if _, err := body.Seek(0, io.SeekStart); err != nil {
			return "", err
		}
		// If-None-Match: * makes the PUT fail rather than overwrite.
		resp, err := n.request(ctx, c, http.MethodPut, davBase(c)+"/"+rel, io.NopCloser(body), map[string]string{
			"Content-Type":   contentType,
			"If-None-Match":  "*",
			"Content-Length": strconv.FormatInt(size, 10),
		})
		if err != nil {
			return "", err
		}
		resp.Body.Close()
		if resp.StatusCode == http.StatusPreconditionFailed {
			continue
		}
		if err := statusErr(resp, http.StatusCreated, http.StatusNoContent); err != nil {
			return "", err
		}
		p, _ := url.PathUnescape(rel)
		return "/" + p, nil
	}
	return "", errors.New("Nextcloud: too many files with that name")
}

func (n *Nextcloud) ensureFolder(ctx context.Context, c settings.Nextcloud) error {
	folder := folderPath(c.Folder)
	if folder == "" {
		return nil
	}
	path := ""
	for _, part := range strings.Split(folder, "/") {
		path += "/" + part
		resp, err := n.request(ctx, c, "MKCOL", davBase(c)+path, nil, nil)
		if err != nil {
			return err
		}
		resp.Body.Close()
		// 405: it already exists.
		if err := statusErr(resp, http.StatusCreated, http.StatusMethodNotAllowed); err != nil {
			return err
		}
	}
	return nil
}

func (n *Nextcloud) request(ctx context.Context, c settings.Nextcloud, method, u string, body io.Reader, headers map[string]string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, method, u, body)
	if err != nil {
		return nil, err
	}
	if cl := headers["Content-Length"]; cl != "" {
		req.ContentLength, _ = strconv.ParseInt(cl, 10, 64)
		delete(headers, "Content-Length")
	}
	req.SetBasicAuth(c.User, c.Password)
	req.Header.Set("OCS-APIRequest", "true")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := n.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("Nextcloud: %w", err)
	}
	return resp, nil
}

func statusErr(resp *http.Response, ok ...int) error {
	for _, code := range ok {
		if resp.StatusCode == code {
			return nil
		}
	}
	switch resp.StatusCode {
	case http.StatusUnauthorized:
		return errors.New("Nextcloud refused the user or app password")
	case http.StatusNotFound:
		return errors.New("Nextcloud: not found; check the address and user")
	case http.StatusInsufficientStorage:
		return errors.New("Nextcloud: out of space")
	}
	return fmt.Errorf("Nextcloud: %s %s: HTTP %s", resp.Request.Method, resp.Request.URL.Path, resp.Status)
}

func splitExt(name string) (string, string) {
	if i := strings.LastIndex(name, "."); i > 0 {
		return name[:i], name[i:]
	}
	return name, ""
}
