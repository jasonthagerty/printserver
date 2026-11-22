package cups

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/OpenPrinting/goipp"
)

func find(attrs goipp.Attributes, name string) (goipp.Values, bool) {
	for _, a := range attrs {
		if a.Name == name {
			return a.Values, true
		}
	}
	return nil, false
}

func strs(attrs goipp.Attributes, name string) []string {
	vals, _ := find(attrs, name)
	out := make([]string, 0, len(vals))
	for _, v := range vals {
		if s, ok := v.V.(goipp.String); ok {
			out = append(out, string(s))
		}
	}
	return out
}

func firstString(attrs goipp.Attributes, name string) string {
	if s := strs(attrs, name); len(s) > 0 {
		return s[0]
	}
	return ""
}

func ints(attrs goipp.Attributes, name string) []int {
	vals, _ := find(attrs, name)
	out := make([]int, 0, len(vals))
	for _, v := range vals {
		if i, ok := v.V.(goipp.Integer); ok {
			out = append(out, int(i))
		}
	}
	return out
}

func firstInt(attrs goipp.Attributes, name string, def int) int {
	if i := ints(attrs, name); len(i) > 0 {
		return i[0]
	}
	return def
}

func firstBool(attrs goipp.Attributes, name string) bool {
	vals, _ := find(attrs, name)
	for _, v := range vals {
		if b, ok := v.V.(goipp.Boolean); ok {
			return bool(b)
		}
	}
	return false
}

func keywords(name string, values ...string) goipp.Attribute {
	a := goipp.Attribute{Name: name}
	for _, v := range values {
		a.Values.Add(goipp.TagKeyword, goipp.String(v))
	}
	return a
}

func unixTime(sec int) time.Time {
	if sec <= 0 {
		return time.Time{}
	}
	return time.Unix(int64(sec), 0).UTC()
}

// filterReasons drops "none", which CUPS sends when there is no reason. The
// -report/-warning/-error suffixes are kept for callers to interpret.
func filterReasons(in []string) []string {
	out := in[:0]
	for _, r := range in {
		if r != "none" && r != "" {
			out = append(out, r)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func printerState(v int) string {
	switch v {
	case 3:
		return "idle"
	case 4:
		return "processing"
	case 5:
		return "stopped"
	}
	return "unknown"
}

func jobState(v int) string {
	switch v {
	case 3:
		return "pending"
	case 4:
		return "held"
	case 5:
		return "processing"
	case 6:
		return "stopped"
	case 7:
		return "canceled"
	case 8:
		return "aborted"
	case 9:
		return "completed"
	}
	return "unknown"
}

func qualityName(v int) string {
	switch v {
	case 3:
		return "draft"
	case 4:
		return "normal"
	case 5:
		return "high"
	}
	return ""
}

func qualityEnum(s string) int {
	switch s {
	case "draft":
		return 3
	case "normal":
		return 4
	case "high":
		return 5
	}
	return 0
}

// parsePageRanges turns "1-3,5" into a page-ranges attribute.
func parsePageRanges(s string) (goipp.Attribute, error) {
	a := goipp.Attribute{Name: "page-ranges"}
	for _, part := range strings.Split(s, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		lo, hi, isRange := strings.Cut(part, "-")
		from, err := strconv.Atoi(strings.TrimSpace(lo))
		if err != nil || from < 1 {
			return a, fmt.Errorf("%w: page range %q", ErrInvalidOption, part)
		}
		to := from
		if isRange {
			if to, err = strconv.Atoi(strings.TrimSpace(hi)); err != nil || to < from {
				return a, fmt.Errorf("%w: page range %q", ErrInvalidOption, part)
			}
		}
		a.Values.Add(goipp.TagRange, goipp.Range{Lower: from, Upper: to})
	}
	if len(a.Values) == 0 {
		return a, fmt.Errorf("%w: page range %q", ErrInvalidOption, s)
	}
	return a, nil
}
