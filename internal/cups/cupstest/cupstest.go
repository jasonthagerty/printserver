// Package cupstest is an in-memory IPP server that answers the requests
// internal/cups sends, for tests.
package cupstest

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"time"

	"github.com/OpenPrinting/goipp"
)

// Job is a job the fake received.
type Job struct {
	ID        int
	Name      string
	Format    string
	Data      []byte
	State     int // IPP job-state enum
	JobAttrs  goipp.Attributes
	Created   time.Time
	Completed time.Time
}

// Server is the fake. Set fields before sending requests.
type Server struct {
	*httptest.Server

	mu           sync.Mutex
	nextID       int
	Jobs         []*Job
	StateReasons []string
	MarkerLevels []int
	// Requests counts operations by code.
	Requests map[goipp.Op]int
}

// New starts a fake.
func New() *Server {
	s := &Server{nextID: 1, StateReasons: []string{"none"}, MarkerLevels: []int{40, 75}, Requests: map[goipp.Op]int{}}
	s.Server = httptest.NewServer(http.HandlerFunc(s.handle))
	return s
}

// Received returns a copy of the jobs.
func (s *Server) Received() []Job {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Job, len(s.Jobs))
	for i, j := range s.Jobs {
		out[i] = *j
	}
	return out
}

// SetReasons replaces printer-state-reasons.
func (s *Server) SetReasons(r ...string) {
	s.mu.Lock()
	s.StateReasons = r
	s.mu.Unlock()
}

// Complete marks job id completed.
func (s *Server) Complete(id int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, j := range s.Jobs {
		if j.ID == id {
			j.State = 9
			// Later completions sort later, even within one second.
			j.Completed = time.Unix(1_800_000_000+int64(id), 0)
		}
	}
}

func (s *Server) handle(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	var req goipp.Message
	rd := bytes.NewReader(body)
	if err := req.Decode(rd); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	doc, _ := io.ReadAll(rd)

	s.mu.Lock()
	defer s.mu.Unlock()
	op := goipp.Op(req.Code)
	s.Requests[op]++
	resp := goipp.NewResponse(goipp.DefaultVersion, goipp.StatusOk, req.RequestID)
	resp.Operation.Add(goipp.MakeAttribute("attributes-charset", goipp.TagCharset, goipp.String("utf-8")))
	resp.Operation.Add(goipp.MakeAttribute("attributes-natural-language", goipp.TagLanguage, goipp.String("en-US")))

	switch op {
	case goipp.OpGetPrinterAttributes:
		p := &resp.Printer
		p.Add(goipp.MakeAttribute("printer-name", goipp.TagName, goipp.String("MG3620")))
		p.Add(goipp.MakeAttribute("printer-info", goipp.TagText, goipp.String("Canon MG3620")))
		p.Add(goipp.MakeAttribute("printer-make-and-model", goipp.TagText, goipp.String("Canon PIXMA MG3620 - CUPS+Gutenprint v5.3.4")))
		p.Add(goipp.MakeAttribute("printer-state", goipp.TagEnum, goipp.Integer(3)))
		reasons := goipp.Attribute{Name: "printer-state-reasons"}
		for _, rr := range s.StateReasons {
			reasons.Values.Add(goipp.TagKeyword, goipp.String(rr))
		}
		p.Add(reasons)
		p.Add(goipp.MakeAttribute("printer-is-accepting-jobs", goipp.TagBoolean, goipp.Boolean(true)))
		p.Add(goipp.MakeAttribute("queued-job-count", goipp.TagInteger, goipp.Integer(s.active())))
		p.Add(goipp.MakeAttr("marker-names", goipp.TagName, goipp.String("Black"), goipp.String("Color")))
		p.Add(goipp.MakeAttr("marker-colors", goipp.TagName, goipp.String("#000000"), goipp.String("#00FFFF#FF00FF#FFFF00")))
		levels := goipp.Attribute{Name: "marker-levels"}
		for _, l := range s.MarkerLevels {
			levels.Values.Add(goipp.TagInteger, goipp.Integer(l))
		}
		p.Add(levels)
		p.Add(goipp.MakeAttr("media-supported", goipp.TagKeyword,
			goipp.String("na_letter_8.5x11in"), goipp.String("na_index-4x6_4x6in"), goipp.String("iso_a4_210x297mm"), goipp.String("custom_weird_1x1in")))
		p.Add(goipp.MakeAttribute("media-default", goipp.TagKeyword, goipp.String("na_letter_8.5x11in")))
		p.Add(goipp.MakeAttr("sides-supported", goipp.TagKeyword, goipp.String("one-sided"), goipp.String("two-sided-long-edge"), goipp.String("two-sided-short-edge")))
		p.Add(goipp.MakeAttr("print-color-mode-supported", goipp.TagKeyword, goipp.String("monochrome"), goipp.String("color")))
		p.Add(goipp.MakeAttr("print-quality-supported", goipp.TagEnum, goipp.Integer(3), goipp.Integer(4), goipp.Integer(5)))
	case goipp.OpGetJobs:
		which := ""
		for _, a := range req.Operation {
			if a.Name == "which-jobs" {
				which = a.Values[0].V.String()
			}
		}
		for _, j := range s.Jobs {
			done := j.State >= 7
			if (which == "completed") != done {
				continue
			}
			var g goipp.Attributes
			g.Add(goipp.MakeAttribute("job-id", goipp.TagInteger, goipp.Integer(j.ID)))
			g.Add(goipp.MakeAttribute("job-name", goipp.TagName, goipp.String(j.Name)))
			g.Add(goipp.MakeAttribute("job-originating-user-name", goipp.TagName, goipp.String("web")))
			g.Add(goipp.MakeAttribute("job-state", goipp.TagEnum, goipp.Integer(j.State)))
			g.Add(goipp.MakeAttribute("job-state-reasons", goipp.TagKeyword, goipp.String("none")))
			g.Add(goipp.MakeAttribute("time-at-creation", goipp.TagInteger, goipp.Integer(j.Created.Unix())))
			if !j.Completed.IsZero() {
				g.Add(goipp.MakeAttribute("time-at-completed", goipp.TagInteger, goipp.Integer(j.Completed.Unix())))
			}
			resp.Groups = append(resp.Groups, goipp.Group{Tag: goipp.TagJobGroup, Attrs: g})
		}
		if resp.Groups != nil {
			resp.Groups = append(goipp.Groups{{Tag: goipp.TagOperationGroup, Attrs: resp.Operation}}, resp.Groups...)
		}
	case goipp.OpPrintJob:
		j := &Job{ID: s.nextID, Data: doc, State: 3, JobAttrs: req.Job, Created: time.Now()}
		for _, a := range req.Operation {
			switch a.Name {
			case "job-name":
				j.Name = a.Values[0].V.String()
			case "document-format":
				j.Format = a.Values[0].V.String()
			}
		}
		s.nextID++
		s.Jobs = append(s.Jobs, j)
		resp.Job.Add(goipp.MakeAttribute("job-id", goipp.TagInteger, goipp.Integer(j.ID)))
	case goipp.OpCancelJob:
		id := 0
		for _, a := range req.Operation {
			if a.Name == "job-id" {
				id = int(a.Values[0].V.(goipp.Integer))
			}
		}
		resp.Code = goipp.Code(goipp.StatusErrorNotFound)
		for _, j := range s.Jobs {
			if j.ID == id {
				if j.State >= 7 {
					resp.Code = goipp.Code(goipp.StatusErrorNotPossible)
				} else {
					j.State = 7
					resp.Code = goipp.Code(goipp.StatusOk)
				}
			}
		}
	default:
		resp.Code = goipp.Code(goipp.StatusErrorOperationNotSupported)
	}
	out, err := resp.EncodeBytes()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", goipp.ContentType)
	_, _ = w.Write(out)
}

func (s *Server) active() int {
	n := 0
	for _, j := range s.Jobs {
		if j.State < 7 {
			n++
		}
	}
	return n
}
