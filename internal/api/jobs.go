package api

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/jasonthagerty/printserver/internal/cups"
)

// Job is a print job, as the queue screen shows it.
type Job struct {
	ID          int        `json:"id"`
	Name        string     `json:"name"`
	User        string     `json:"user"`
	State       string     `json:"state" enum:"pending,held,processing,stopped,canceled,aborted,completed,unknown"`
	Reasons     []string   `json:"reasons"`
	Message     string     `json:"message"`
	CreatedAt   *time.Time `json:"created_at"`
	CompletedAt *time.Time `json:"completed_at"`
	Pages       int        `json:"pages" doc:"sheets printed so far"`
	Cancelable  bool       `json:"cancelable"`
}

// JobsOutput lists the queue and recent history.
type JobsOutput struct {
	Body struct {
		Active []Job `json:"active"`
		Recent []Job `json:"recent" doc:"finished jobs, newest first"`
	}
}

// Option is one choice in a print setting.
type Option struct {
	Value string `json:"value"`
	Label string `json:"label"`
}

// PrintOptionsOutput is what the print form offers.
type PrintOptionsOutput struct {
	Body struct {
		Media        []Option `json:"media"`
		MediaDefault string   `json:"media_default"`
		Sides        []Option `json:"sides"`
		ColorModes   []Option `json:"color_modes"`
		Qualities    []Option `json:"qualities"`
		Accept       []string `json:"accept" doc:"file types the server can print"`
	}
}

// PrintForm is the multipart upload.
type PrintForm struct {
	File    huma.FormFile `form:"file" required:"true" doc:"PDF, JPEG, PNG or plain text"`
	Copies  int           `form:"copies" required:"false" minimum:"1" maximum:"99" default:"1"`
	Color   string        `form:"color" required:"false" enum:"color,monochrome" default:"color"`
	Sides   string        `form:"sides" required:"false" enum:"one-sided,two-sided-long-edge,two-sided-short-edge" default:"one-sided"`
	Media   string        `form:"media" required:"false" maxLength:"80" doc:"media keyword; empty uses the printer default"`
	Quality string        `form:"quality" required:"false" enum:"draft,normal,high" default:"normal"`
	Scaling string        `form:"scaling" required:"false" enum:"auto,fit,fill,none" default:"auto"`
	Pages   string        `form:"pages" required:"false" maxLength:"100" doc:"page ranges such as 1-3,5; empty prints all"`
}

// PrintInput is the upload request.
type PrintInput struct {
	RawBody huma.MultipartFormFiles[PrintForm]
}

// PrintOutput names the new job.
type PrintOutput struct {
	Body struct {
		ID int `json:"id"`
	}
}

// JobIDInput selects a job.
type JobIDInput struct {
	ID int `path:"id" minimum:"1"`
}

// printable maps sniffed content types to what CUPS is told.
var printable = map[string]string{
	"application/pdf": "application/pdf",
	"image/jpeg":      "image/jpeg",
	"image/png":       "image/png",
	"text/plain":      "text/plain",
}

var acceptExt = []string{".pdf", ".jpg", ".jpeg", ".png", ".txt"}

func registerJobs(api huma.API, deps Deps) {
	huma.Register(api, huma.Operation{
		OperationID: "list-jobs",
		Method:      http.MethodGet,
		Path:        "/jobs",
		Summary:     "Print queue and recent jobs",
		Tags:        []string{"Printing"},
	}, func(ctx context.Context, _ *struct{}) (*JobsOutput, error) {
		active, err := deps.CUPS.Jobs(ctx, false, 0)
		if err != nil {
			return nil, cupsError(deps, err)
		}
		recent, err := deps.CUPS.Jobs(ctx, true, 10)
		if err != nil {
			return nil, cupsError(deps, err)
		}
		out := &JobsOutput{}
		out.Body.Active, out.Body.Recent = toJobs(active), toJobs(recent)
		return out, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "get-print-options",
		Method:      http.MethodGet,
		Path:        "/print-options",
		Summary:     "Settings the printer supports",
		Tags:        []string{"Printing"},
	}, func(ctx context.Context, _ *struct{}) (*PrintOptionsOutput, error) {
		p, err := deps.CUPS.Printer(ctx)
		if err != nil {
			return nil, cupsError(deps, err)
		}
		out := &PrintOptionsOutput{}
		b := &out.Body
		b.Media, b.MediaDefault = mediaOptions(p.Media, p.MediaDefault)
		b.Sides = labeled(p.Sides, sideLabels, []string{"one-sided"})
		b.ColorModes = labeled(p.ColorModes, colorLabels, []string{"color", "monochrome"})
		b.Qualities = labeled(p.Qualities, qualityLabels, []string{"normal"})
		b.Accept = append([]string{"application/pdf", "image/jpeg", "image/png", "text/plain"}, acceptExt...)
		return out, nil
	})

	huma.Register(api, huma.Operation{
		OperationID:   "print",
		Method:        http.MethodPost,
		Path:          "/jobs",
		Summary:       "Print an uploaded file",
		Tags:          []string{"Printing"},
		DefaultStatus: http.StatusCreated,
		MaxBodyBytes:  maxUpload,
	}, func(ctx context.Context, in *PrintInput) (*PrintOutput, error) {
		form := in.RawBody.Data()
		f := form.File
		defer f.Close()
		format, err := sniff(f)
		if err != nil {
			return nil, huma.Error422UnprocessableEntity(err.Error())
		}
		name := strings.TrimSpace(filepath.Base(f.Filename))
		if name == "" || name == "." || name == "/" {
			name = "Upload"
		}
		opts := cups.PrintOptions{
			Name:      name,
			Format:    format,
			Copies:    form.Copies,
			ColorMode: form.Color,
			Sides:     form.Sides,
			Media:     form.Media,
			Quality:   form.Quality,
			PageRange: strings.TrimSpace(form.Pages),
		}
		if form.Scaling != "auto" {
			opts.Scaling = form.Scaling
		}
		id, err := deps.CUPS.Print(ctx, f, opts)
		if err != nil {
			return nil, cupsError(deps, err)
		}
		deps.Log.Info("print job", "id", id, "name", name, "format", format)
		out := &PrintOutput{}
		out.Body.ID = id
		return out, nil
	})

	huma.Register(api, huma.Operation{
		OperationID:   "cancel-job",
		Method:        http.MethodDelete,
		Path:          "/jobs/{id}",
		Summary:       "Cancel a print job",
		Tags:          []string{"Printing"},
		DefaultStatus: http.StatusNoContent,
	}, func(ctx context.Context, in *JobIDInput) (*struct{}, error) {
		if err := deps.CUPS.Cancel(ctx, in.ID); err != nil {
			return nil, cupsError(deps, err)
		}
		return nil, nil
	})
}

// sniff detects the file type from its content, not its name: a phone may
// send any name, and CUPS must be told the truth.
func sniff(f io.ReadSeeker) (string, error) {
	var head [512]byte
	n, err := io.ReadFull(f, head[:])
	if err != nil && !errors.Is(err, io.ErrUnexpectedEOF) && !errors.Is(err, io.EOF) {
		return "", err
	}
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return "", err
	}
	if n == 0 {
		return "", errors.New("the file is empty")
	}
	ct := http.DetectContentType(head[:n])
	if i := strings.IndexByte(ct, ';'); i >= 0 {
		ct = ct[:i]
	}
	if bytes.HasPrefix(head[:n], []byte("%PDF-")) {
		ct = "application/pdf"
	}
	if mt, ok := printable[ct]; ok {
		return mt, nil
	}
	return "", fmt.Errorf("can't print this kind of file (%s); use PDF, JPEG, PNG or plain text", ct)
}

func toJobs(in []cups.Job) []Job {
	out := make([]Job, 0, len(in))
	for _, j := range in {
		job := Job{
			ID:         j.ID,
			Name:       j.Name,
			User:       j.User,
			State:      j.State,
			Reasons:    j.StateReasons,
			Message:    j.PrinterStateMsg,
			Pages:      j.PagesCompleted,
			Cancelable: j.Active(),
		}
		if job.Reasons == nil {
			job.Reasons = []string{}
		}
		// A filter that fails on a document stops the job (CUPS does not
		// retry or abort it); it does not hold up the queue, but it stays
		// until cancelled, with an error only a developer can read.
		if j.State == "stopped" && slices.Contains(j.StateReasons, "job-completed-with-errors") {
			job.Message = "This file couldn't be printed. Cancel it and try saving it as a PDF again."
		}
		if !j.CreatedAt.IsZero() {
			t := j.CreatedAt
			job.CreatedAt = &t
		}
		if !j.CompletedAt.IsZero() {
			t := j.CompletedAt
			job.CompletedAt = &t
		}
		out = append(out, job)
	}
	return out
}

var sideLabels = map[string]string{
	"one-sided":            "One-sided",
	"two-sided-long-edge":  "Two-sided (book)",
	"two-sided-short-edge": "Two-sided (flip up)",
}

var colorLabels = map[string]string{
	"color":      "Color",
	"monochrome": "Black and white",
}

var qualityLabels = map[string]string{
	"draft":  "Draft",
	"normal": "Normal",
	"high":   "Best",
}

// mediaLabels are the sizes worth offering on a phone, in display order.
var mediaLabels = []Option{
	{"na_letter_8.5x11in", "Letter"},
	{"na_legal_8.5x14in", "Legal"},
	{"iso_a4_210x297mm", "A4"},
	{"na_index-4x6_4x6in", "4 × 6 photo"},
	{"na_5x7_5x7in", "5 × 7 photo"},
	{"na_govt-letter_8x10in", "8 × 10 photo"},
	{"om_small-photo_100x150mm", "10 × 15 cm photo"},
	{"na_number-10_4.125x9.5in", "#10 envelope"},
}

func mediaOptions(supported []string, def string) ([]Option, string) {
	var out []Option
	for _, m := range mediaLabels {
		if slices.Contains(supported, m.Value) {
			out = append(out, m)
		}
	}
	if def != "" && !slices.ContainsFunc(out, func(o Option) bool { return o.Value == def }) {
		out = append([]Option{{def, def}}, out...)
	}
	if len(out) == 0 {
		out = []Option{{"na_letter_8.5x11in", "Letter"}}
	}
	if def == "" {
		def = out[0].Value
	}
	return out, def
}

func labeled(values []string, labels map[string]string, fallback []string) []Option {
	if len(values) == 0 {
		values = fallback
	}
	out := make([]Option, 0, len(values))
	for _, v := range values {
		l, ok := labels[v]
		if !ok {
			continue
		}
		out = append(out, Option{v, l})
	}
	return out
}

func cupsError(deps Deps, err error) error {
	switch {
	case errors.Is(err, cups.ErrInvalidOption):
		return huma.Error422UnprocessableEntity(strings.TrimPrefix(err.Error(), "invalid print option: ") + " is not valid; use pages like 1-3,5")
	case cups.IsNotFound(err):
		return huma.Error404NotFound("no such job")
	case cups.IsNotPossible(err):
		return huma.Error409Conflict("that job has already finished")
	}
	var ce *cups.Error
	if errors.As(err, &ce) {
		return huma.Error422UnprocessableEntity("the printer refused: " + ce.Message)
	}
	deps.Log.Warn("cups request failed", "err", err)
	return huma.Error503ServiceUnavailable("The print service is not answering. Try again in a minute.")
}
