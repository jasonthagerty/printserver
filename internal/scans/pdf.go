package scans

import (
	"bufio"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"io"
	"os"
)

// pdfPage is one JPEG to place on its own page. The file is opened by the
// caller while it holds the store's lock, so a page removed or renumbered
// meanwhile cannot change what is written.
type pdfPage struct {
	f   *os.File
	dpi int
}

// writePDF writes a PDF with one page per JPEG, each page the size of the
// scanned area. The JPEG data is embedded as-is (DCTDecode), so nothing is
// re-encoded and the router does no image work.
func writePDF(w io.Writer, pages []pdfPage) error {
	bw := bufio.NewWriter(w)
	var offsets []int64
	var pos int64
	write := func(format string, args ...any) error {
		n, err := fmt.Fprintf(bw, format, args...)
		pos += int64(n)
		return err
	}
	obj := func() int {
		offsets = append(offsets, pos)
		return len(offsets)
	}

	if err := write("%%PDF-1.4\n%%\xe2\xe3\xcf\xd3\n"); err != nil {
		return err
	}
	// Object 1: catalog, 2: page tree. Each page uses three objects: image,
	// content stream, page.
	n := len(pages)
	pageObj := func(i int) int { return 3 + i*3 + 2 }

	obj()
	if err := write("1 0 obj\n<< /Type /Catalog /Pages 2 0 R >>\nendobj\n"); err != nil {
		return err
	}
	obj()
	if err := write("2 0 obj\n<< /Type /Pages /Count %d /Kids [", n); err != nil {
		return err
	}
	for i := range pages {
		if err := write(" %d 0 R", pageObj(i)); err != nil {
			return err
		}
	}
	if err := write(" ] >>\nendobj\n"); err != nil {
		return err
	}

	for _, p := range pages {
		cfg, size, err := jpegFileInfo(p.f)
		if err != nil {
			return err
		}
		dpi := p.dpi
		if dpi <= 0 {
			dpi = 300
		}
		wPt := float64(cfg.Width) * 72 / float64(dpi)
		hPt := float64(cfg.Height) * 72 / float64(dpi)
		cs := "/DeviceRGB"
		switch cfg.ColorModel {
		case color.GrayModel, color.Gray16Model:
			cs = "/DeviceGray"
		case color.CMYKModel:
			// Adobe-style CMYK JPEGs store inverted values.
			cs = "/DeviceCMYK /Decode [1 0 1 0 1 0 1 0]"
		}

		imgID := obj()
		if err := write("%d 0 obj\n<< /Type /XObject /Subtype /Image /Width %d /Height %d /ColorSpace %s /BitsPerComponent 8 /Filter /DCTDecode /Length %d >>\nstream\n",
			imgID, cfg.Width, cfg.Height, cs, size); err != nil {
			return err
		}
		copied, err := io.Copy(bw, io.NewSectionReader(p.f, 0, size))
		if err != nil {
			return err
		}
		pos += copied
		if err := write("\nendstream\nendobj\n"); err != nil {
			return err
		}

		content := fmt.Sprintf("q %.2f 0 0 %.2f 0 0 cm /Im0 Do Q\n", wPt, hPt)
		contentID := obj()
		if err := write("%d 0 obj\n<< /Length %d >>\nstream\n%sendstream\nendobj\n", contentID, len(content), content); err != nil {
			return err
		}
		pid := obj()
		if err := write("%d 0 obj\n<< /Type /Page /Parent 2 0 R /MediaBox [0 0 %.2f %.2f] /Resources << /XObject << /Im0 %d 0 R >> >> /Contents %d 0 R >>\nendobj\n",
			pid, wPt, hPt, imgID, contentID); err != nil {
			return err
		}
	}

	xref := pos
	if err := write("xref\n0 %d\n0000000000 65535 f \n", len(offsets)+1); err != nil {
		return err
	}
	for _, off := range offsets {
		if err := write("%010d 00000 n \n", off); err != nil {
			return err
		}
	}
	if err := write("trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(offsets)+1, xref); err != nil {
		return err
	}
	return bw.Flush()
}

func jpegInfo(path string) (image.Config, int64, error) {
	f, err := os.Open(path)
	if err != nil {
		return image.Config{}, 0, err
	}
	defer f.Close()
	return jpegFileInfo(f)
}

func jpegFileInfo(f *os.File) (image.Config, int64, error) {
	st, err := f.Stat()
	if err != nil {
		return image.Config{}, 0, err
	}
	cfg, err := jpeg.DecodeConfig(io.NewSectionReader(f, 0, st.Size()))
	if err != nil {
		return image.Config{}, 0, fmt.Errorf("%s: %w", f.Name(), err)
	}
	return cfg, st.Size(), nil
}
