// Package scans keeps scanned documents on disk: one directory per document
// with its pages as JPEGs and a meta.json. A PDF is assembled from the pages
// when downloaded or sent, so adding and removing pages is cheap.
package scans

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/jasonthagerty/printserver/internal/escl"
)

// Scanner is the part of the eSCL client the store needs.
type Scanner interface {
	Capabilities(ctx context.Context) (escl.Capabilities, error)
	Scan(ctx context.Context, s escl.Settings) (io.ReadCloser, string, error)
}

// Errors callers map to HTTP statuses.
var (
	ErrNotFound = errors.New("scan not found")
	ErrBusy     = errors.New("the scanner is busy")
	ErrInvalid  = errors.New("invalid scan request")
)

// MaxPages bounds a document, so a stuck "add page" loop cannot fill the
// drive.
const MaxPages = 50

// Options choose how a page is scanned.
type Options struct {
	Resolution int    // dpi
	Color      string // color or gray
	Format     string // pdf or jpeg (the document's file type)
}

// Doc is one scanned document.
type Doc struct {
	ID         string     `json:"id"`
	Name       string     `json:"name"`
	CreatedAt  time.Time  `json:"created_at"`
	UpdatedAt  time.Time  `json:"updated_at"`
	Format     string     `json:"format"`
	Color      string     `json:"color"`
	Resolution int        `json:"resolution"`
	Pages      int        `json:"pages"`
	Bytes      int64      `json:"bytes"`
	Deliveries []Delivery `json:"deliveries,omitempty"`
}

// Delivery records a send to a destination.
type Delivery struct {
	Destination string    `json:"destination"`
	Path        string    `json:"path"`
	At          time.Time `json:"at"`
}

// FileName is the document's download name.
func (d Doc) FileName() string {
	ext := ".pdf"
	if d.Format == "jpeg" {
		ext = ".jpg"
	}
	return safeName(d.Name) + ext
}

// ContentType is the MIME type of the document's file.
func (d Doc) ContentType() string {
	if d.Format == "jpeg" {
		return "image/jpeg"
	}
	return "application/pdf"
}

// Store is the scans directory.
type Store struct {
	dir     string
	scanner Scanner
	log     *slog.Logger
	now     func() time.Time

	scanMu sync.Mutex // held while the scanner is in use
	metaMu sync.Mutex // guards meta.json reads and writes
}

// NewStore opens (and creates) dir.
func NewStore(dir string, scanner Scanner, log *slog.Logger) (*Store, error) {
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return nil, err
	}
	return &Store{dir: dir, scanner: scanner, log: log, now: time.Now}, nil
}

var idPattern = regexp.MustCompile(`^[0-9]{8}-[0-9]{6}-[0-9a-f]{6}$`)

func (s *Store) docDir(id string) (string, error) {
	if !idPattern.MatchString(id) {
		return "", ErrNotFound
	}
	return filepath.Join(s.dir, id), nil
}

func pageFile(n int) string { return fmt.Sprintf("page-%03d.jpg", n) }

// List returns every document, newest first.
func (s *Store) List() ([]Doc, error) {
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		return nil, err
	}
	docs := []Doc{}
	for _, e := range entries {
		if !e.IsDir() || !idPattern.MatchString(e.Name()) {
			continue
		}
		d, err := s.Get(e.Name())
		if err != nil {
			s.log.Warn("skipping unreadable scan", "id", e.Name(), "err", err)
			continue
		}
		docs = append(docs, d)
	}
	sort.Slice(docs, func(i, j int) bool { return docs[i].CreatedAt.After(docs[j].CreatedAt) })
	return docs, nil
}

// Get returns one document.
func (s *Store) Get(id string) (Doc, error) {
	dir, err := s.docDir(id)
	if err != nil {
		return Doc{}, err
	}
	s.metaMu.Lock()
	defer s.metaMu.Unlock()
	return readMeta(dir)
}

func readMeta(dir string) (Doc, error) {
	b, err := os.ReadFile(filepath.Join(dir, "meta.json"))
	if errors.Is(err, fs.ErrNotExist) {
		return Doc{}, ErrNotFound
	}
	if err != nil {
		return Doc{}, err
	}
	var d Doc
	if err := json.Unmarshal(b, &d); err != nil {
		return Doc{}, fmt.Errorf("meta.json: %w", err)
	}
	return d, nil
}

func writeMeta(dir string, d Doc) error {
	b, err := json.MarshalIndent(d, "", "  ")
	if err != nil {
		return err
	}
	return writeFileAtomic(filepath.Join(dir, "meta.json"), b, 0o640)
}

// Capabilities returns what the scanner supports, for the scan form.
func (s *Store) Capabilities(ctx context.Context) (escl.Capabilities, error) {
	return s.scanner.Capabilities(ctx)
}

// New scans a page into a new document.
func (s *Store) New(ctx context.Context, o Options) (Doc, error) {
	if o.Format != "pdf" && o.Format != "jpeg" {
		return Doc{}, fmt.Errorf("%w: format must be pdf or jpeg", ErrInvalid)
	}
	id, err := s.newID()
	if err != nil {
		return Doc{}, err
	}
	dir := filepath.Join(s.dir, id)
	if err := os.Mkdir(dir, 0o750); err != nil {
		return Doc{}, err
	}
	res, err := s.scanPage(ctx, o, filepath.Join(dir, pageFile(1)))
	if err != nil {
		_ = os.RemoveAll(dir)
		return Doc{}, err
	}
	now := s.now().UTC()
	d := Doc{
		ID:         id,
		Name:       "Scan " + now.Local().Format("2006-01-02 15.04.05"),
		CreatedAt:  now,
		UpdatedAt:  now,
		Format:     o.Format,
		Color:      o.Color,
		Resolution: res,
		Pages:      1,
	}
	d.Bytes = dirSize(dir)
	s.metaMu.Lock()
	defer s.metaMu.Unlock()
	if err := writeMeta(dir, d); err != nil {
		_ = os.RemoveAll(dir)
		return Doc{}, err
	}
	return d, nil
}

// AddPage scans another page onto a PDF document, with its settings.
func (s *Store) AddPage(ctx context.Context, id string) (Doc, error) {
	d, err := s.Get(id)
	if err != nil {
		return Doc{}, err
	}
	if d.Format != "pdf" {
		return Doc{}, fmt.Errorf("%w: only PDF scans have more than one page", ErrInvalid)
	}
	if d.Pages >= MaxPages {
		return Doc{}, fmt.Errorf("%w: a scan can have at most %d pages", ErrInvalid, MaxPages)
	}
	dir, _ := s.docDir(id)
	f, err := os.CreateTemp(dir, ".incoming-*.jpg")
	if err != nil {
		return Doc{}, err
	}
	tmp := f.Name()
	f.Close()
	if _, err := s.scanPage(ctx, Options{Resolution: d.Resolution, Color: d.Color, Format: d.Format}, tmp); err != nil {
		_ = os.Remove(tmp)
		return Doc{}, err
	}
	s.metaMu.Lock()
	defer s.metaMu.Unlock()
	d, err = readMeta(dir) // it may have changed while scanning
	if err == nil && d.Pages >= MaxPages {
		err = fmt.Errorf("%w: a scan can have at most %d pages", ErrInvalid, MaxPages)
	}
	if err != nil {
		_ = os.Remove(tmp)
		return Doc{}, err
	}
	if err := os.Rename(tmp, filepath.Join(dir, pageFile(d.Pages+1))); err != nil {
		_ = os.Remove(tmp)
		return Doc{}, err
	}
	d.Pages++
	d.UpdatedAt = s.now().UTC()
	d.Bytes = dirSize(dir)
	return d, writeMeta(dir, d)
}

// RemovePage deletes page n (1-based) and renumbers the rest. A document
// keeps at least one page; delete the document instead.
func (s *Store) RemovePage(id string, n int) (Doc, error) {
	dir, err := s.docDir(id)
	if err != nil {
		return Doc{}, err
	}
	s.metaMu.Lock()
	defer s.metaMu.Unlock()
	d, err := readMeta(dir)
	if err != nil {
		return Doc{}, err
	}
	if n < 1 || n > d.Pages {
		return Doc{}, ErrNotFound
	}
	if d.Pages == 1 {
		return Doc{}, fmt.Errorf("%w: a scan needs at least one page; delete the scan instead", ErrInvalid)
	}
	if err := os.Remove(filepath.Join(dir, pageFile(n))); err != nil {
		return Doc{}, err
	}
	for i := n + 1; i <= d.Pages; i++ {
		if err := os.Rename(filepath.Join(dir, pageFile(i)), filepath.Join(dir, pageFile(i-1))); err != nil {
			return Doc{}, err
		}
	}
	d.Pages--
	d.UpdatedAt = s.now().UTC()
	d.Bytes = dirSize(dir)
	return d, writeMeta(dir, d)
}

// Rename sets the document's name.
func (s *Store) Rename(id, name string) (Doc, error) {
	name = strings.TrimSpace(name)
	if name == "" || len(name) > 120 {
		return Doc{}, fmt.Errorf("%w: name must be 1 to 120 characters", ErrInvalid)
	}
	dir, err := s.docDir(id)
	if err != nil {
		return Doc{}, err
	}
	s.metaMu.Lock()
	defer s.metaMu.Unlock()
	d, err := readMeta(dir)
	if err != nil {
		return Doc{}, err
	}
	d.Name = name
	d.UpdatedAt = s.now().UTC()
	return d, writeMeta(dir, d)
}

// RecordDelivery notes that the document was sent somewhere.
func (s *Store) RecordDelivery(id string, del Delivery) (Doc, error) {
	dir, err := s.docDir(id)
	if err != nil {
		return Doc{}, err
	}
	s.metaMu.Lock()
	defer s.metaMu.Unlock()
	d, err := readMeta(dir)
	if err != nil {
		return Doc{}, err
	}
	d.Deliveries = append(d.Deliveries, del)
	return d, writeMeta(dir, d)
}

// Delete removes a document.
func (s *Store) Delete(id string) error {
	dir, err := s.docDir(id)
	if err != nil {
		return err
	}
	s.metaMu.Lock()
	defer s.metaMu.Unlock()
	if _, err := os.Stat(filepath.Join(dir, "meta.json")); err != nil {
		return ErrNotFound
	}
	return os.RemoveAll(dir)
}

// OpenPage opens the JPEG of page n, for previews. The caller closes it.
func (s *Store) OpenPage(id string, n int) (*os.File, error) {
	dir, err := s.docDir(id)
	if err != nil {
		return nil, err
	}
	s.metaMu.Lock()
	defer s.metaMu.Unlock()
	d, err := readMeta(dir)
	if err != nil {
		return nil, err
	}
	if n < 1 || n > d.Pages {
		return nil, ErrNotFound
	}
	return os.Open(filepath.Join(dir, pageFile(n)))
}

// Write writes the document's file (PDF or JPEG) to w. The pages are opened
// under the lock, so a concurrent edit or delete cannot corrupt the output.
func (s *Store) Write(id string, w io.Writer) (Doc, error) {
	d, files, err := s.openPages(id)
	if err != nil {
		return Doc{}, err
	}
	defer func() {
		for _, f := range files {
			f.Close()
		}
	}()
	if d.Format == "jpeg" {
		_, err = io.Copy(w, files[0])
		return d, err
	}
	pages := make([]pdfPage, len(files))
	for i, f := range files {
		pages[i] = pdfPage{f: f, dpi: d.Resolution}
	}
	return d, writePDF(w, pages)
}

func (s *Store) openPages(id string) (Doc, []*os.File, error) {
	dir, err := s.docDir(id)
	if err != nil {
		return Doc{}, nil, err
	}
	s.metaMu.Lock()
	defer s.metaMu.Unlock()
	d, err := readMeta(dir)
	if err != nil {
		return Doc{}, nil, err
	}
	n := d.Pages
	if d.Format == "jpeg" {
		n = 1
	}
	files := make([]*os.File, 0, n)
	for i := 1; i <= n; i++ {
		f, err := os.Open(filepath.Join(dir, pageFile(i)))
		if err != nil {
			for _, f := range files {
				f.Close()
			}
			return Doc{}, nil, err
		}
		files = append(files, f)
	}
	return d, files, nil
}

// TempFile creates a scratch file on the data drive (not in memory), which
// Cleanup removes if it is left behind.
func (s *Store) TempFile() (*os.File, error) {
	return os.CreateTemp(s.dir, ".tmp-*")
}

// ScanTo scans one page as a JPEG into w without storing it (for copying).
func (s *Store) ScanTo(ctx context.Context, o Options, w io.Writer) error {
	tmp, err := os.CreateTemp(s.dir, ".tmp-copy-*.jpg")
	if err != nil {
		return err
	}
	name := tmp.Name()
	tmp.Close()
	defer os.Remove(name)
	if _, err := s.scanPage(ctx, o, name); err != nil {
		return err
	}
	f, err := os.Open(name)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = io.Copy(w, f)
	return err
}

// Cleanup deletes documents created before maxAge ago, and leftovers of
// interrupted scans.
func (s *Store) Cleanup(maxAge time.Duration) (int, error) {
	docs, err := s.List()
	if err != nil {
		return 0, err
	}
	cutoff := s.now().Add(-maxAge)
	removed := 0
	for _, d := range docs {
		if d.CreatedAt.Before(cutoff) {
			if err := s.Delete(d.ID); err != nil && !errors.Is(err, ErrNotFound) {
				return removed, err
			}
			removed++
		}
	}
	// A crash between mkdir and meta.json leaves a directory with no meta.
	entries, _ := os.ReadDir(s.dir)
	for _, e := range entries {
		p := filepath.Join(s.dir, e.Name())
		info, err := e.Info()
		if err != nil || s.now().Sub(info.ModTime()) < time.Hour {
			continue
		}
		if strings.HasPrefix(e.Name(), ".tmp-") {
			_ = os.Remove(p)
			continue
		}
		if e.IsDir() && idPattern.MatchString(e.Name()) {
			if _, err := os.Stat(filepath.Join(p, "meta.json")); errors.Is(err, fs.ErrNotExist) {
				_ = os.RemoveAll(p)
			}
		}
	}
	return removed, nil
}

// scanPage scans one JPEG page to path and returns the resolution used.
func (s *Store) scanPage(ctx context.Context, o Options, path string) (int, error) {
	if !s.scanMu.TryLock() {
		return 0, ErrBusy
	}
	defer s.scanMu.Unlock()

	caps, err := s.scanner.Capabilities(ctx)
	if err != nil {
		return 0, err
	}
	settings, err := settingsFor(caps, o)
	if err != nil {
		return 0, err
	}
	body, _, err := s.scanner.Scan(ctx, settings)
	if errors.Is(err, escl.ErrBusy) {
		return 0, ErrBusy
	}
	if err != nil {
		return 0, err
	}
	defer body.Close()

	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o640)
	if err != nil {
		return 0, err
	}
	// A 600 dpi color page of the full glass is well under 100 MB as JPEG.
	const maxPage = 200 << 20
	n, err := io.Copy(f, io.LimitReader(body, maxPage+1))
	if err == nil && n > maxPage {
		err = errors.New("the scanned page is larger than 200 MB")
	}
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		_, _, err = jpegInfo(path)
	}
	if err != nil {
		_ = os.Remove(path)
		return 0, fmt.Errorf("receiving scan: %w", err)
	}
	return settings.Resolution, nil
}

func settingsFor(caps escl.Capabilities, o Options) (escl.Settings, error) {
	mode := "RGB24"
	if o.Color == "gray" {
		mode = "Grayscale8"
	} else if o.Color != "color" {
		return escl.Settings{}, fmt.Errorf("%w: color must be color or gray", ErrInvalid)
	}
	if len(caps.ColorModes) > 0 && !slices.Contains(caps.ColorModes, mode) {
		return escl.Settings{}, fmt.Errorf("%w: the scanner does not support %s", ErrInvalid, mode)
	}
	if len(caps.Formats) > 0 && !slices.Contains(caps.Formats, "image/jpeg") {
		return escl.Settings{}, errors.New("the scanner does not offer JPEG")
	}
	res := o.Resolution
	if len(caps.Resolutions) > 0 && !slices.Contains(caps.Resolutions, res) {
		// Use the highest supported resolution not above the request.
		best := caps.Resolutions[0]
		for _, r := range caps.Resolutions {
			if r <= res {
				best = r
			}
		}
		res = best
	}
	if res <= 0 {
		return escl.Settings{}, fmt.Errorf("%w: resolution must be positive", ErrInvalid)
	}
	return escl.Settings{
		Resolution: res,
		ColorMode:  mode,
		Format:     "image/jpeg",
		Width:      caps.MaxWidth,
		Height:     caps.MaxHeight,
	}, nil
}

func (s *Store) newID() (string, error) {
	var b [3]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return s.now().UTC().Format("20060102-150405") + "-" + hex.EncodeToString(b[:]), nil
}

func dirSize(dir string) int64 {
	var n int64
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), "page-") {
			if info, err := e.Info(); err == nil {
				n += info.Size()
			}
		}
	}
	return n
}

var unsafeChars = regexp.MustCompile(`[^\p{L}\p{N} ._()-]+`)

// safeName makes a name usable as a file name everywhere: no separators or
// control characters, no leading dot.
func safeName(name string) string {
	s := strings.TrimSpace(unsafeChars.ReplaceAllString(name, "_"))
	s = strings.TrimLeft(s, ".")
	if s == "" {
		return "scan"
	}
	return s
}

func writeFileAtomic(path string, data []byte, perm fs.FileMode) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(name)
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		os.Remove(name)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(name)
		return err
	}
	if err := os.Chmod(name, perm); err != nil {
		os.Remove(name)
		return err
	}
	return os.Rename(name, path)
}
