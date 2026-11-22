package api

import (
	"bytes"
	"context"
	"errors"
	"io"
	"mime"
	"net/http"
	"os"
	"slices"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/jasonthagerty/printserver/internal/cups"
	"github.com/jasonthagerty/printserver/internal/destinations"
	"github.com/jasonthagerty/printserver/internal/escl"
	"github.com/jasonthagerty/printserver/internal/scans"
)

// A scan can take a minute at 600 dpi; a request must not outlive that by
// much, or a stuck scanner would hold the lock.
const scanTimeout = 3 * time.Minute

// ScannerOutput is what the scan form offers.
type ScannerOutput struct {
	Body struct {
		Available   bool     `json:"available"`
		Model       string   `json:"model"`
		Resolutions []int    `json:"resolutions" doc:"dpi values offered, ascending"`
		Colors      []string `json:"colors" doc:"subset of color, gray"`
		Formats     []string `json:"formats" doc:"subset of pdf, jpeg"`
	}
}

// ScanDoc is a scanned document.
type ScanDoc struct {
	ID         string     `json:"id"`
	Name       string     `json:"name"`
	CreatedAt  time.Time  `json:"created_at"`
	UpdatedAt  time.Time  `json:"updated_at"`
	Format     string     `json:"format" enum:"pdf,jpeg"`
	Color      string     `json:"color" enum:"color,gray"`
	Resolution int        `json:"resolution"`
	Pages      int        `json:"pages"`
	Bytes      int64      `json:"bytes"`
	FileName   string     `json:"file_name"`
	Deliveries []Delivery `json:"deliveries"`
}

// Delivery is a past send of a scan.
type Delivery struct {
	Destination string    `json:"destination" enum:"nextcloud"`
	Path        string    `json:"path"`
	At          time.Time `json:"at"`
}

// ScanDocOutput returns one document.
type ScanDocOutput struct {
	Body ScanDoc
}

// ScansOutput lists documents.
type ScansOutput struct {
	Body struct {
		Scans []ScanDoc `json:"scans" doc:"newest first"`
	}
}

// ScanRequest starts a new document.
type ScanRequest struct {
	Body struct {
		Resolution int    `json:"resolution" minimum:"75" maximum:"600" default:"300"`
		Color      string `json:"color" enum:"color,gray" default:"color"`
		Format     string `json:"format" enum:"pdf,jpeg" default:"pdf"`
	}
}

// ScanIDInput selects a document.
type ScanIDInput struct {
	ID string `path:"id" pattern:"^[0-9]{8}-[0-9]{6}-[0-9a-f]{6}$"`
}

// ScanPageInput selects a page.
type ScanPageInput struct {
	ID   string `path:"id" pattern:"^[0-9]{8}-[0-9]{6}-[0-9a-f]{6}$"`
	Page int    `path:"page" minimum:"1"`
}

// RenameInput renames a document.
type RenameInput struct {
	ID   string `path:"id" pattern:"^[0-9]{8}-[0-9]{6}-[0-9a-f]{6}$"`
	Body struct {
		Name string `json:"name" minLength:"1" maxLength:"120"`
	}
}

// FileInput downloads a document.
type FileInput struct {
	ID       string `path:"id" pattern:"^[0-9]{8}-[0-9]{6}-[0-9a-f]{6}$"`
	Download bool   `query:"download" doc:"send as an attachment instead of for viewing"`
}

// SendInput sends a document to a destination.
type SendInput struct {
	ID   string `path:"id" pattern:"^[0-9]{8}-[0-9]{6}-[0-9a-f]{6}$"`
	Body struct {
		Destination string `json:"destination" enum:"nextcloud"`
	}
}

// SendOutput says where the file went.
type SendOutput struct {
	Body struct {
		Path string  `json:"path" doc:"where the file was stored at the destination"`
		Scan ScanDoc `json:"scan"`
	}
}

// PrintScanInput prints a scanned document.
type PrintScanInput struct {
	ID   string `path:"id" pattern:"^[0-9]{8}-[0-9]{6}-[0-9a-f]{6}$"`
	Body CopySettings
}

// CopySettings are the print side of a copy.
type CopySettings struct {
	Copies int    `json:"copies" minimum:"1" maximum:"99" default:"1"`
	Color  string `json:"color" enum:"color,monochrome" default:"color"`
}

// CopyInput scans the glass and prints it.
type CopyInput struct {
	Body CopySettings
}

func registerScans(api huma.API, deps Deps) {
	huma.Register(api, huma.Operation{
		OperationID: "get-scanner",
		Method:      http.MethodGet,
		Path:        "/scanner",
		Summary:     "Scanner capabilities",
		Tags:        []string{"Scanning"},
	}, func(ctx context.Context, _ *struct{}) (*ScannerOutput, error) {
		out := &ScannerOutput{}
		b := &out.Body
		b.Resolutions, b.Colors, b.Formats = []int{}, []string{}, []string{}
		caps, err := deps.Scans.Capabilities(ctx)
		if err != nil {
			deps.Log.Info("scanner unavailable", "err", err)
			return out, nil
		}
		b.Available, b.Model = true, caps.MakeAndModel
		for _, r := range caps.Resolutions {
			// Offer the useful steps only: 150 for quick, 300 for documents,
			// 600 for photos. Higher is slow and huge on a flatbed this size.
			if slices.Contains([]int{75, 150, 300, 600}, r) {
				b.Resolutions = append(b.Resolutions, r)
			}
		}
		if len(b.Resolutions) == 0 {
			b.Resolutions = caps.Resolutions
		}
		if len(caps.ColorModes) == 0 || slices.Contains(caps.ColorModes, "RGB24") {
			b.Colors = append(b.Colors, "color")
		}
		if len(caps.ColorModes) == 0 || slices.Contains(caps.ColorModes, "Grayscale8") {
			b.Colors = append(b.Colors, "gray")
		}
		if len(caps.Formats) == 0 || slices.Contains(caps.Formats, "image/jpeg") {
			b.Formats = []string{"pdf", "jpeg"}
		}
		return out, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "list-scans",
		Method:      http.MethodGet,
		Path:        "/scans",
		Summary:     "Recent scans",
		Tags:        []string{"Scanning"},
	}, func(ctx context.Context, _ *struct{}) (*ScansOutput, error) {
		docs, err := deps.Scans.List()
		if err != nil {
			return nil, scanError(deps, err)
		}
		out := &ScansOutput{}
		out.Body.Scans = make([]ScanDoc, 0, len(docs))
		for _, d := range docs {
			out.Body.Scans = append(out.Body.Scans, toScanDoc(d))
		}
		return out, nil
	})

	huma.Register(api, huma.Operation{
		OperationID:   "create-scan",
		Method:        http.MethodPost,
		Path:          "/scans",
		Summary:       "Scan a page into a new document",
		Description:   "Blocks until the page is scanned, which can take up to a minute.",
		Tags:          []string{"Scanning"},
		DefaultStatus: http.StatusCreated,
	}, func(ctx context.Context, in *ScanRequest) (*ScanDocOutput, error) {
		ctx, cancel := context.WithTimeout(ctx, scanTimeout)
		defer cancel()
		d, err := deps.Scans.New(ctx, scans.Options{
			Resolution: in.Body.Resolution, Color: in.Body.Color, Format: in.Body.Format,
		})
		if err != nil {
			return nil, scanError(deps, err)
		}
		return &ScanDocOutput{Body: toScanDoc(d)}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "add-scan-page",
		Method:      http.MethodPost,
		Path:        "/scans/{id}/pages",
		Summary:     "Scan another page onto a PDF",
		Tags:        []string{"Scanning"},
	}, func(ctx context.Context, in *ScanIDInput) (*ScanDocOutput, error) {
		ctx, cancel := context.WithTimeout(ctx, scanTimeout)
		defer cancel()
		d, err := deps.Scans.AddPage(ctx, in.ID)
		if err != nil {
			return nil, scanError(deps, err)
		}
		return &ScanDocOutput{Body: toScanDoc(d)}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "delete-scan-page",
		Method:      http.MethodDelete,
		Path:        "/scans/{id}/pages/{page}",
		Summary:     "Remove a page",
		Tags:        []string{"Scanning"},
	}, func(ctx context.Context, in *ScanPageInput) (*ScanDocOutput, error) {
		d, err := deps.Scans.RemovePage(in.ID, in.Page)
		if err != nil {
			return nil, scanError(deps, err)
		}
		return &ScanDocOutput{Body: toScanDoc(d)}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "get-scan-page",
		Method:      http.MethodGet,
		Path:        "/scans/{id}/pages/{page}",
		Summary:     "A page's image (JPEG)",
		Tags:        []string{"Scanning"},
		Responses: map[string]*huma.Response{
			"200": {Description: "The page", Content: map[string]*huma.MediaType{"image/jpeg": {}}},
		},
	}, func(ctx context.Context, in *ScanPageInput) (*huma.StreamResponse, error) {
		f, err := deps.Scans.OpenPage(in.ID, in.Page)
		if err != nil {
			return nil, scanError(deps, err)
		}
		return &huma.StreamResponse{Body: func(hctx huma.Context) {
			defer f.Close()
			hctx.SetHeader("Content-Type", "image/jpeg")
			// Pages never change once scanned (removing one renumbers the
			// rest, so cache only briefly).
			hctx.SetHeader("Cache-Control", "private, max-age=60")
			_, _ = io.Copy(hctx.BodyWriter(), f)
		}}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "get-scan-file",
		Method:      http.MethodGet,
		Path:        "/scans/{id}/file",
		Summary:     "Download a scan as PDF or JPEG",
		Tags:        []string{"Scanning"},
		Responses: map[string]*huma.Response{
			"200": {Description: "The document", Content: map[string]*huma.MediaType{
				"application/pdf": {}, "image/jpeg": {},
			}},
		},
	}, func(ctx context.Context, in *FileInput) (*huma.StreamResponse, error) {
		d, err := deps.Scans.Get(in.ID)
		if err != nil {
			return nil, scanError(deps, err)
		}
		return &huma.StreamResponse{Body: func(hctx huma.Context) {
			disp := "inline"
			if in.Download {
				disp = "attachment"
			}
			hctx.SetHeader("Content-Type", d.ContentType())
			hctx.SetHeader("Content-Disposition", mime.FormatMediaType(disp, map[string]string{"filename": d.FileName()}))
			if _, err := deps.Scans.Write(in.ID, hctx.BodyWriter()); err != nil {
				deps.Log.Warn("writing scan", "id", in.ID, "err", err)
			}
		}}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "rename-scan",
		Method:      http.MethodPatch,
		Path:        "/scans/{id}",
		Summary:     "Rename a scan",
		Tags:        []string{"Scanning"},
	}, func(ctx context.Context, in *RenameInput) (*ScanDocOutput, error) {
		d, err := deps.Scans.Rename(in.ID, in.Body.Name)
		if err != nil {
			return nil, scanError(deps, err)
		}
		return &ScanDocOutput{Body: toScanDoc(d)}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID:   "delete-scan",
		Method:        http.MethodDelete,
		Path:          "/scans/{id}",
		Summary:       "Delete a scan",
		Tags:          []string{"Scanning"},
		DefaultStatus: http.StatusNoContent,
	}, func(ctx context.Context, in *ScanIDInput) (*struct{}, error) {
		if err := deps.Scans.Delete(in.ID); err != nil {
			return nil, scanError(deps, err)
		}
		return nil, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "send-scan",
		Method:      http.MethodPost,
		Path:        "/scans/{id}/send",
		Summary:     "Send a scan to Nextcloud",
		Tags:        []string{"Scanning"},
	}, func(ctx context.Context, in *SendInput) (*SendOutput, error) {
		// A long PDF is built on the drive, not in memory, and re-read for
		// each upload attempt.
		tmp, err := deps.Scans.TempFile()
		if err != nil {
			return nil, scanError(deps, err)
		}
		defer os.Remove(tmp.Name())
		defer tmp.Close()
		d, err := deps.Scans.Write(in.ID, tmp)
		if err != nil {
			return nil, scanError(deps, err)
		}
		path, err := deps.Nextcloud.Upload(ctx, deps.Settings.Get().Nextcloud, d.FileName(), tmp, d.ContentType())
		if errors.Is(err, destinations.ErrNotConfigured) {
			return nil, huma.Error409Conflict(err.Error())
		}
		if err != nil {
			deps.Log.Warn("nextcloud upload", "id", in.ID, "err", err)
			return nil, huma.Error502BadGateway(err.Error())
		}
		d, err = deps.Scans.RecordDelivery(in.ID, scans.Delivery{Destination: "nextcloud", Path: path, At: time.Now().UTC()})
		if err != nil {
			return nil, scanError(deps, err)
		}
		out := &SendOutput{}
		out.Body.Path, out.Body.Scan = path, toScanDoc(d)
		return out, nil
	})

	huma.Register(api, huma.Operation{
		OperationID:   "print-scan",
		Method:        http.MethodPost,
		Path:          "/scans/{id}/print",
		Summary:       "Print a scan",
		Tags:          []string{"Scanning"},
		DefaultStatus: http.StatusCreated,
	}, func(ctx context.Context, in *PrintScanInput) (*PrintOutput, error) {
		d, err := deps.Scans.Get(in.ID)
		if err != nil {
			return nil, scanError(deps, err)
		}
		// Stream the PDF into the print request instead of holding it.
		pr, pw := io.Pipe()
		go func() {
			_, err := deps.Scans.Write(in.ID, pw)
			pw.CloseWithError(err)
		}()
		defer pr.Close()
		id, err := deps.CUPS.Print(ctx, pr, cups.PrintOptions{
			Name: d.FileName(), Format: d.ContentType(), Copies: in.Body.Copies,
			ColorMode: in.Body.Color, Scaling: "fit",
		})
		if err != nil {
			return nil, cupsError(deps, err)
		}
		out := &PrintOutput{}
		out.Body.ID = id
		return out, nil
	})

	huma.Register(api, huma.Operation{
		OperationID:   "copy",
		Method:        http.MethodPost,
		Path:          "/copy",
		Summary:       "Copy: scan the glass and print it",
		Description:   "Blocks until the page is scanned and queued.",
		Tags:          []string{"Scanning"},
		DefaultStatus: http.StatusCreated,
	}, func(ctx context.Context, in *CopyInput) (*PrintOutput, error) {
		sctx, cancel := context.WithTimeout(ctx, scanTimeout)
		defer cancel()
		color := "color"
		if in.Body.Color == "monochrome" {
			color = "gray"
		}
		var buf bytes.Buffer
		if err := deps.Scans.ScanTo(sctx, scans.Options{Resolution: 300, Color: color, Format: "jpeg"}, &buf); err != nil {
			return nil, scanError(deps, err)
		}
		id, err := deps.CUPS.Print(ctx, &buf, cups.PrintOptions{
			Name: "Copy", Format: "image/jpeg", Copies: in.Body.Copies,
			ColorMode: in.Body.Color, Scaling: "fit",
		})
		if err != nil {
			return nil, cupsError(deps, err)
		}
		out := &PrintOutput{}
		out.Body.ID = id
		return out, nil
	})
}

func toScanDoc(d scans.Doc) ScanDoc {
	out := ScanDoc{
		ID: d.ID, Name: d.Name, CreatedAt: d.CreatedAt, UpdatedAt: d.UpdatedAt,
		Format: d.Format, Color: d.Color, Resolution: d.Resolution, Pages: d.Pages,
		Bytes: d.Bytes, FileName: d.FileName(), Deliveries: []Delivery{},
	}
	for _, del := range d.Deliveries {
		out.Deliveries = append(out.Deliveries, Delivery{Destination: del.Destination, Path: del.Path, At: del.At})
	}
	return out
}

func scanError(deps Deps, err error) error {
	switch {
	case errors.Is(err, scans.ErrNotFound):
		return huma.Error404NotFound("no such scan")
	case errors.Is(err, scans.ErrBusy):
		return huma.Error409Conflict("The scanner is busy. Try again in a moment.")
	case errors.Is(err, scans.ErrInvalid):
		return huma.Error422UnprocessableEntity(err.Error())
	case errors.Is(err, escl.ErrUnavailable):
		return huma.Error503ServiceUnavailable("The scanner isn't available. Is the printer on?")
	case errors.Is(err, context.DeadlineExceeded):
		return huma.Error504GatewayTimeout("The scanner took too long. Try again.")
	}
	deps.Log.Warn("scan failed", "err", err)
	return huma.Error500InternalServerError("The scan failed. Try again; if it keeps failing, check the server log.")
}
