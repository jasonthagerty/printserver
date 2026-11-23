// Package cups talks IPP to the local cupsd: printer status, the job list,
// printing a document, and cancelling a job. It is the only code that
// speaks IPP; everything else uses these types.
package cups

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync/atomic"
	"time"

	"github.com/OpenPrinting/goipp"
)

// Client is an IPP client for one CUPS queue.
type Client struct {
	http    *http.Client
	baseURL string // where requests are POSTed, e.g. http://localhost
	queue   string
	reqID   atomic.Uint32
}

// NewSocket returns a client that reaches cupsd over its domain socket, so
// it is trusted as a local client and needs no network listener.
func NewSocket(socket, queue string) *Client {
	tr := &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			var d net.Dialer
			return d.DialContext(ctx, "unix", socket)
		},
		MaxIdleConns:    4,
		IdleConnTimeout: 30 * time.Second,
	}
	return &Client{http: &http.Client{Transport: tr}, baseURL: "http://localhost", queue: queue}
}

// NewHTTP returns a client for a CUPS server at baseURL (e.g.
// http://127.0.0.1:631), for development and tests.
func NewHTTP(baseURL, queue string, hc *http.Client) *Client {
	if hc == nil {
		hc = &http.Client{}
	}
	return &Client{http: hc, baseURL: strings.TrimSuffix(baseURL, "/"), queue: queue}
}

// Queue is the CUPS queue name.
func (c *Client) Queue() string { return c.queue }

func (c *Client) printerURI() string {
	return "ipp://localhost/printers/" + url.PathEscape(c.queue)
}

// Error is an IPP-level failure: the server answered with a non-success
// status.
type Error struct {
	Status  goipp.Status
	Message string
}

func (e *Error) Error() string {
	if e.Message != "" {
		return fmt.Sprintf("cups: %s: %s", e.Status, e.Message)
	}
	return fmt.Sprintf("cups: %s", e.Status)
}

// ErrInvalidOption marks a print setting the client got wrong, such as a
// malformed page range.
var ErrInvalidOption = errors.New("invalid print option")

// IsNotFound reports whether err is CUPS saying the job or printer does not
// exist.
func IsNotFound(err error) bool {
	var e *Error
	return errors.As(err, &e) && e.Status == goipp.StatusErrorNotFound
}

// IsNotPossible reports whether CUPS refused because of the object's state,
// such as cancelling a job that already finished.
func IsNotPossible(err error) bool {
	var e *Error
	return errors.As(err, &e) && e.Status == goipp.StatusErrorNotPossible
}

func (c *Client) newRequest(op goipp.Op) *goipp.Message {
	m := goipp.NewRequest(goipp.DefaultVersion, op, c.reqID.Add(1))
	m.Operation.Add(goipp.MakeAttribute("attributes-charset", goipp.TagCharset, goipp.String("utf-8")))
	m.Operation.Add(goipp.MakeAttribute("attributes-natural-language", goipp.TagLanguage, goipp.String("en-US")))
	return m
}

// do sends req, followed by doc if it is not nil, and decodes the response.
func (c *Client) do(ctx context.Context, path string, req *goipp.Message, doc io.Reader) (*goipp.Message, error) {
	head, err := req.EncodeBytes()
	if err != nil {
		return nil, fmt.Errorf("cups: encode: %w", err)
	}
	var body io.Reader = bytes.NewReader(head)
	if doc != nil {
		body = io.MultiReader(body, doc)
	}
	hr, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+path, body)
	if err != nil {
		return nil, err
	}
	hr.Header.Set("Content-Type", goipp.ContentType)
	resp, err := c.http.Do(hr)
	if err != nil {
		return nil, fmt.Errorf("cups: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("cups: HTTP %s", resp.Status)
	}
	var m goipp.Message
	if err := m.Decode(resp.Body); err != nil {
		return nil, fmt.Errorf("cups: decode: %w", err)
	}
	if st := goipp.Status(m.Code); st >= 0x0100 { // 0x00xx are successful statuses
		return nil, &Error{Status: st, Message: firstString(m.Operation, "status-message")}
	}
	return &m, nil
}

// Printer is the queue's state and what it supports.
type Printer struct {
	Name          string
	Info          string
	Location      string
	UUID          string   // urn:uuid:...
	URF           []string // urf-supported, for AirPrint
	Type          int      // CUPS printer-type bits
	Color         bool
	MakeAndModel  string
	DeviceURI     string
	State         string // idle, processing, stopped
	StateReasons  []string
	StateMessage  string
	Accepting     bool
	QueuedJobs    int
	Markers       []Marker
	Media         []string // media-supported keywords
	MediaDefault  string
	Sides         []string
	ColorModes    []string
	Qualities     []string // draft, normal, high
	DocumentTypes []string
}

// Marker is one ink or toner supply, as the driver reports it.
type Marker struct {
	Name  string
	Color string // #RRGGBB, or "" when unknown
	Type  string
	Level int // percent; -1 unknown
}

var printerAttrs = []string{
	"printer-name", "printer-info", "printer-make-and-model", "device-uri",
	"printer-state", "printer-state-reasons", "printer-state-message",
	"printer-is-accepting-jobs", "queued-job-count",
	"marker-names", "marker-colors", "marker-types", "marker-levels",
	"media-supported", "media-default", "sides-supported",
	"print-color-mode-supported", "print-quality-supported",
	"document-format-supported", "printer-location", "printer-uuid",
	"urf-supported", "printer-type", "color-supported",
}

// Printer returns the queue's current state.
func (c *Client) Printer(ctx context.Context) (Printer, error) {
	req := c.newRequest(goipp.OpGetPrinterAttributes)
	req.Operation.Add(goipp.MakeAttribute("printer-uri", goipp.TagURI, goipp.String(c.printerURI())))
	req.Operation.Add(keywords("requested-attributes", printerAttrs...))
	resp, err := c.do(ctx, "/printers/"+url.PathEscape(c.queue), req, nil)
	if err != nil {
		return Printer{}, err
	}
	a := resp.Printer
	p := Printer{
		Name:          firstString(a, "printer-name"),
		Info:          firstString(a, "printer-info"),
		Location:      firstString(a, "printer-location"),
		UUID:          firstString(a, "printer-uuid"),
		URF:           strs(a, "urf-supported"),
		Type:          firstInt(a, "printer-type", 0),
		Color:         firstBool(a, "color-supported"),
		MakeAndModel:  firstString(a, "printer-make-and-model"),
		DeviceURI:     firstString(a, "device-uri"),
		State:         printerState(firstInt(a, "printer-state", 0)),
		StateReasons:  filterReasons(strs(a, "printer-state-reasons")),
		StateMessage:  firstString(a, "printer-state-message"),
		Accepting:     firstBool(a, "printer-is-accepting-jobs"),
		QueuedJobs:    firstInt(a, "queued-job-count", 0),
		Media:         strs(a, "media-supported"),
		MediaDefault:  firstString(a, "media-default"),
		Sides:         strs(a, "sides-supported"),
		ColorModes:    strs(a, "print-color-mode-supported"),
		DocumentTypes: strs(a, "document-format-supported"),
	}
	for _, q := range ints(a, "print-quality-supported") {
		if s := qualityName(q); s != "" {
			p.Qualities = append(p.Qualities, s)
		}
	}
	names, colors, types, levels := strs(a, "marker-names"), strs(a, "marker-colors"), strs(a, "marker-types"), ints(a, "marker-levels")
	for i, n := range names {
		m := Marker{Name: n, Level: -1}
		if i < len(colors) && strings.HasPrefix(colors[i], "#") {
			m.Color = colors[i]
		}
		if i < len(types) {
			m.Type = types[i]
		}
		if i < len(levels) && levels[i] >= 0 && levels[i] <= 100 {
			m.Level = levels[i]
		}
		p.Markers = append(p.Markers, m)
	}
	return p, nil
}

// Job is one print job.
type Job struct {
	ID              int
	Name            string
	User            string
	State           string // pending, held, processing, stopped, canceled, aborted, completed
	StateReasons    []string
	CreatedAt       time.Time
	CompletedAt     time.Time // zero while not finished
	PagesCompleted  int
	PrinterStateMsg string
}

// Active reports whether the job has not finished yet.
func (j Job) Active() bool {
	switch j.State {
	case "pending", "held", "processing", "stopped":
		return true
	}
	return false
}

var jobAttrs = []string{
	"job-id", "job-name", "job-originating-user-name", "job-state", "job-state-reasons",
	"time-at-creation", "time-at-completed", "job-media-sheets-completed",
	"job-printer-state-message",
}

// Jobs lists the queue's jobs. With completed, it lists finished jobs, most
// recent first, up to limit (0 for all); otherwise the jobs still waiting or
// printing, oldest first.
func (c *Client) Jobs(ctx context.Context, completed bool, limit int) ([]Job, error) {
	req := c.newRequest(goipp.OpGetJobs)
	req.Operation.Add(goipp.MakeAttribute("printer-uri", goipp.TagURI, goipp.String(c.printerURI())))
	which := "not-completed"
	if completed {
		which = "completed"
	}
	req.Operation.Add(goipp.MakeAttribute("which-jobs", goipp.TagKeyword, goipp.String(which)))
	// No IPP limit: CUPS applies it to the oldest jobs first, so a limit would
	// hide the newest finished ones. History is bounded by MaxJobs anyway.
	req.Operation.Add(keywords("requested-attributes", jobAttrs...))
	resp, err := c.do(ctx, "/", req, nil)
	if err != nil {
		return nil, err
	}
	var jobs []Job
	for _, g := range resp.Groups {
		if g.Tag != goipp.TagJobGroup {
			continue
		}
		a := g.Attrs
		j := Job{
			ID:              firstInt(a, "job-id", 0),
			Name:            firstString(a, "job-name"),
			User:            firstString(a, "job-originating-user-name"),
			State:           jobState(firstInt(a, "job-state", 0)),
			StateReasons:    filterReasons(strs(a, "job-state-reasons")),
			CreatedAt:       unixTime(firstInt(a, "time-at-creation", 0)),
			CompletedAt:     unixTime(firstInt(a, "time-at-completed", 0)),
			PagesCompleted:  firstInt(a, "job-media-sheets-completed", 0),
			PrinterStateMsg: firstString(a, "job-printer-state-message"),
		}
		if j.ID != 0 {
			jobs = append(jobs, j)
		}
	}
	if completed {
		sort.SliceStable(jobs, func(i, k int) bool {
			if !jobs[i].CompletedAt.Equal(jobs[k].CompletedAt) {
				return jobs[i].CompletedAt.After(jobs[k].CompletedAt)
			}
			return jobs[i].ID > jobs[k].ID
		})
	}
	if limit > 0 && len(jobs) > limit {
		jobs = jobs[:limit]
	}
	return jobs, nil
}

// PrintOptions are the job settings the web UI offers. Zero values leave the
// queue's default.
type PrintOptions struct {
	Name      string // job name shown in queues
	User      string // requesting-user-name
	Format    string // MIME type; "" lets CUPS detect it
	Copies    int
	ColorMode string // color or monochrome
	Sides     string // one-sided, two-sided-long-edge, two-sided-short-edge
	Media     string // media keyword, e.g. na_letter_8.5x11in
	Quality   string // draft, normal, high
	Scaling   string // auto, fit, fill, none
	PageRange string // e.g. "1-3,5"; "" prints everything
}

// Print submits doc as a new job and returns its ID.
func (c *Client) Print(ctx context.Context, doc io.Reader, o PrintOptions) (int, error) {
	req := c.newRequest(goipp.OpPrintJob)
	req.Operation.Add(goipp.MakeAttribute("printer-uri", goipp.TagURI, goipp.String(c.printerURI())))
	user := o.User
	if user == "" {
		user = "web"
	}
	req.Operation.Add(goipp.MakeAttribute("requesting-user-name", goipp.TagName, goipp.String(user)))
	if o.Name != "" {
		req.Operation.Add(goipp.MakeAttribute("job-name", goipp.TagName, goipp.String(o.Name)))
	}
	format := o.Format
	if format == "" {
		format = "application/octet-stream"
	}
	req.Operation.Add(goipp.MakeAttribute("document-format", goipp.TagMimeType, goipp.String(format)))

	if o.Copies > 1 {
		req.Job.Add(goipp.MakeAttribute("copies", goipp.TagInteger, goipp.Integer(o.Copies)))
	}
	if o.ColorMode != "" {
		req.Job.Add(goipp.MakeAttribute("print-color-mode", goipp.TagKeyword, goipp.String(o.ColorMode)))
	}
	if o.Sides != "" {
		req.Job.Add(goipp.MakeAttribute("sides", goipp.TagKeyword, goipp.String(o.Sides)))
	}
	if o.Media != "" {
		req.Job.Add(goipp.MakeAttribute("media", goipp.TagKeyword, goipp.String(o.Media)))
	}
	if q := qualityEnum(o.Quality); q != 0 {
		req.Job.Add(goipp.MakeAttribute("print-quality", goipp.TagEnum, goipp.Integer(q)))
	}
	if o.Scaling != "" {
		req.Job.Add(goipp.MakeAttribute("print-scaling", goipp.TagKeyword, goipp.String(o.Scaling)))
	}
	if o.PageRange != "" {
		r, err := parsePageRanges(o.PageRange)
		if err != nil {
			return 0, err
		}
		req.Job.Add(r)
	}
	resp, err := c.do(ctx, "/printers/"+url.PathEscape(c.queue), req, doc)
	if err != nil {
		return 0, err
	}
	id := firstInt(resp.Job, "job-id", 0)
	if id == 0 {
		return 0, errors.New("cups: print accepted but no job-id returned")
	}
	return id, nil
}

// Cancel cancels job id.
func (c *Client) Cancel(ctx context.Context, id int) error {
	req := c.newRequest(goipp.OpCancelJob)
	req.Operation.Add(goipp.MakeAttribute("printer-uri", goipp.TagURI, goipp.String(c.printerURI())))
	req.Operation.Add(goipp.MakeAttribute("job-id", goipp.TagInteger, goipp.Integer(id)))
	req.Operation.Add(goipp.MakeAttribute("requesting-user-name", goipp.TagName, goipp.String("web")))
	_, err := c.do(ctx, "/jobs/", req, nil)
	return err
}
