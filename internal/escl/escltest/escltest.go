// Package escltest is a fake eSCL scanner that returns generated JPEGs, for
// tests.
package escltest

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
)

// Capabilities is what the fake advertises (AirSane's shape, trimmed).
const Capabilities = `<?xml version="1.0" encoding="UTF-8"?>
<scan:ScannerCapabilities xmlns:pwg="http://www.pwg.org/schemas/2010/12/sm" xmlns:scan="http://schemas.hp.com/imaging/escl/2011/05/03">
<pwg:Version>2.63</pwg:Version>
<pwg:MakeAndModel>Canon PIXMA MG3600 Series</pwg:MakeAndModel>
<scan:Platen><scan:PlatenInputCaps>
<scan:MinWidth>16</scan:MinWidth><scan:MaxWidth>2550</scan:MaxWidth>
<scan:MinHeight>16</scan:MinHeight><scan:MaxHeight>3508</scan:MaxHeight>
<scan:SettingProfiles><scan:SettingProfile>
<scan:ColorModes><scan:ColorMode>RGB24</scan:ColorMode><scan:ColorMode>Grayscale8</scan:ColorMode></scan:ColorModes>
<scan:DocumentFormats><pwg:DocumentFormat>image/jpeg</pwg:DocumentFormat><pwg:DocumentFormat>application/pdf</pwg:DocumentFormat><scan:DocumentFormatExt>image/jpeg</scan:DocumentFormatExt></scan:DocumentFormats>
<scan:SupportedResolutions><scan:DiscreteResolutions>
<scan:DiscreteResolution><scan:XResolution>75</scan:XResolution><scan:YResolution>75</scan:YResolution></scan:DiscreteResolution>
<scan:DiscreteResolution><scan:XResolution>150</scan:XResolution><scan:YResolution>150</scan:YResolution></scan:DiscreteResolution>
<scan:DiscreteResolution><scan:XResolution>300</scan:XResolution><scan:YResolution>300</scan:YResolution></scan:DiscreteResolution>
<scan:DiscreteResolution><scan:XResolution>600</scan:XResolution><scan:YResolution>600</scan:YResolution></scan:DiscreteResolution>
<scan:DiscreteResolution><scan:XResolution>1200</scan:XResolution><scan:YResolution>1200</scan:YResolution></scan:DiscreteResolution>
</scan:DiscreteResolutions></scan:SupportedResolutions>
</scan:SettingProfile></scan:SettingProfiles>
</scan:PlatenInputCaps></scan:Platen>
</scan:ScannerCapabilities>`

// Scanner is the fake.
type Scanner struct {
	*httptest.Server

	mu sync.Mutex
	// Busy makes job creation answer 503.
	Busy bool
	// Settings are the ScanSettings bodies received.
	Settings []string
	jobs     map[string]bool
	n        int
}

// New starts a fake. Its eSCL root is URL + "/eSCL".
func New() *Scanner {
	s := &Scanner{jobs: map[string]bool{}}
	s.Server = httptest.NewServer(http.HandlerFunc(s.handle))
	return s
}

// Base is the eSCL root URL.
func (s *Scanner) Base() string { return s.URL + "/eSCL" }

func (s *Scanner) handle(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	switch {
	case r.Method == http.MethodGet && r.URL.Path == "/eSCL/ScannerCapabilities":
		w.Header().Set("Content-Type", "text/xml")
		_, _ = io.WriteString(w, Capabilities)
	case r.Method == http.MethodGet && r.URL.Path == "/eSCL/ScannerStatus":
		state := "Idle"
		if s.Busy {
			state = "Processing"
		}
		fmt.Fprintf(w, `<?xml version="1.0"?><scan:ScannerStatus xmlns:pwg="http://www.pwg.org/schemas/2010/12/sm" xmlns:scan="http://schemas.hp.com/imaging/escl/2011/05/03"><pwg:Version>2.63</pwg:Version><pwg:State>%s</pwg:State></scan:ScannerStatus>`, state)
	case r.Method == http.MethodPost && r.URL.Path == "/eSCL/ScanJobs":
		if s.Busy {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		b, _ := io.ReadAll(r.Body)
		var probe struct{}
		if err := xml.Unmarshal(b, &probe); err != nil {
			http.Error(w, "bad xml", http.StatusBadRequest)
			return
		}
		s.Settings = append(s.Settings, string(b))
		s.n++
		id := fmt.Sprintf("job%d", s.n)
		s.jobs[id] = true
		w.Header().Set("Location", "/eSCL/ScanJobs/"+id)
		w.WriteHeader(http.StatusCreated)
	case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/eSCL/ScanJobs/") && strings.HasSuffix(r.URL.Path, "/NextDocument"):
		id := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/eSCL/ScanJobs/"), "/NextDocument")
		if !s.jobs[id] {
			http.NotFound(w, r)
			return
		}
		delete(s.jobs, id)
		gray := strings.Contains(s.Settings[len(s.Settings)-1], "Grayscale8")
		w.Header().Set("Content-Type", "image/jpeg")
		_, _ = w.Write(Page(gray))
	default:
		http.NotFound(w, r)
	}
}

// Page is a small JPEG standing in for a scanned page.
func Page(gray bool) []byte {
	var buf bytes.Buffer
	if gray {
		img := image.NewGray(image.Rect(0, 0, 85, 110))
		for i := range img.Pix {
			img.Pix[i] = uint8(i % 251)
		}
		_ = jpeg.Encode(&buf, img, nil)
		return buf.Bytes()
	}
	img := image.NewRGBA(image.Rect(0, 0, 85, 110))
	for y := 0; y < 110; y++ {
		for x := 0; x < 85; x++ {
			img.Set(x, y, color.RGBA{uint8(x * 3), uint8(y * 2), 128, 255})
		}
	}
	_ = jpeg.Encode(&buf, img, nil)
	return buf.Bytes()
}
