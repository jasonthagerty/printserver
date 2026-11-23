// Package supplies tracks what wears out on an inkjet that is rarely used:
// how old each cartridge is, and how long since the printer last printed.
// Ink dries in cartridges that sit, so both are worth showing. The printer
// does not report either over USB, so cartridge dates are entered by the
// household ("Replaced today") and the last print is recorded from CUPS.
package supplies

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// DateLayout is how install dates are stored and sent: a calendar day.
const DateLayout = "2006-01-02"

// Cartridge is one ink cartridge.
type Cartridge struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Model       string `json:"model"`
	InstalledOn string `json:"installed_on,omitempty"` // DateLayout; empty when unknown
}

// State is the file's contents.
type State struct {
	Cartridges []Cartridge `json:"cartridges"`
	// WarnMonths is the cartridge age that earns a warning. Canon suggests
	// using a cartridge within about six months of opening it.
	WarnMonths int `json:"warn_months"`
	// NudgeDays suggests a test page after this many days without printing;
	// 0 turns the suggestion off.
	NudgeDays   int        `json:"nudge_days"`
	LastPrinted *time.Time `json:"last_printed,omitempty"`
	LastJobID   int        `json:"last_job_id,omitempty"`
}

// Defaults are the MG3620's two cartridges and the default thresholds.
func Defaults() State {
	return State{
		Cartridges: []Cartridge{
			{ID: "black", Name: "Black", Model: "PG-245 or PG-243"},
			{ID: "color", Name: "Color", Model: "CL-246 or CL-244"},
		},
		WarnMonths: 6,
		NudgeDays:  21,
	}
}

// Errors callers map to HTTP statuses.
var (
	ErrNotFound = errors.New("no such cartridge")
	ErrInvalid  = errors.New("invalid value")
)

// Store is the supplies file.
type Store struct {
	path string
	now  func() time.Time
	mu   sync.Mutex
	cur  State
}

// Open reads path, or starts from defaults.
func Open(path string) (*Store, error) {
	s := &Store{path: path, now: time.Now, cur: Defaults()}
	b, err := os.ReadFile(path)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return s, nil
	case err != nil:
		return nil, err
	}
	var st State
	if err := json.Unmarshal(b, &st); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	// Keep the known cartridges even if the file lacks one.
	for i, c := range s.cur.Cartridges {
		for _, saved := range st.Cartridges {
			if saved.ID == c.ID {
				s.cur.Cartridges[i].InstalledOn = saved.InstalledOn
			}
		}
	}
	if st.WarnMonths > 0 {
		s.cur.WarnMonths = st.WarnMonths
	}
	if st.NudgeDays >= 0 {
		s.cur.NudgeDays = st.NudgeDays
	}
	s.cur.LastPrinted, s.cur.LastJobID = st.LastPrinted, st.LastJobID
	return s, nil
}

// Get returns a copy of the state.
func (s *Store) Get() State {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.copyLocked()
}

func (s *Store) copyLocked() State {
	st := s.cur
	st.Cartridges = append([]Cartridge(nil), s.cur.Cartridges...)
	return st
}

// Today is the current calendar day in the server's time zone.
func (s *Store) Today() string { return s.now().Format(DateLayout) }

// SetInstalled sets a cartridge's install date ("" for unknown).
func (s *Store) SetInstalled(id, date string) (State, error) {
	if date != "" {
		d, err := time.ParseInLocation(DateLayout, date, time.Local)
		if err != nil {
			return State{}, fmt.Errorf("%w: date must look like 2026-11-22", ErrInvalid)
		}
		if d.After(s.now()) {
			return State{}, fmt.Errorf("%w: the date is in the future", ErrInvalid)
		}
	}
	return s.update(func(st *State) error {
		for i := range st.Cartridges {
			if st.Cartridges[i].ID == id {
				st.Cartridges[i].InstalledOn = date
				return nil
			}
		}
		return ErrNotFound
	})
}

// SetThresholds changes the warning age and the test-page suggestion.
func (s *Store) SetThresholds(warnMonths, nudgeDays int) (State, error) {
	if warnMonths < 1 || warnMonths > 60 {
		return State{}, fmt.Errorf("%w: the cartridge warning must be 1 to 60 months", ErrInvalid)
	}
	if nudgeDays < 0 || nudgeDays > 365 {
		return State{}, fmt.Errorf("%w: the test page reminder must be 0 (off) to 365 days", ErrInvalid)
	}
	return s.update(func(st *State) error {
		st.WarnMonths, st.NudgeDays = warnMonths, nudgeDays
		return nil
	})
}

// RecordPrint notes a completed job, if it is newer than the last one seen.
// It reports whether anything changed.
func (s *Store) RecordPrint(jobID int, at time.Time) (bool, error) {
	changed := false
	_, err := s.update(func(st *State) error {
		if st.LastPrinted != nil && !at.After(*st.LastPrinted) {
			return errUnchanged
		}
		t := at.UTC()
		st.LastPrinted, st.LastJobID = &t, jobID
		changed = true
		return nil
	})
	if errors.Is(err, errUnchanged) {
		return false, nil
	}
	return changed, err
}

var errUnchanged = errors.New("unchanged")

func (s *Store) update(f func(*State) error) (State, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	next := s.copyLocked()
	if err := f(&next); err != nil {
		return State{}, err
	}
	b, err := json.MarshalIndent(next, "", "  ")
	if err != nil {
		return State{}, err
	}
	if err := writeAtomic(s.path, b); err != nil {
		return State{}, err
	}
	s.cur = next
	return s.copyLocked(), nil
}

// AgeDays is how many whole days ago date was, or -1 when unknown.
func (s *Store) AgeDays(date string) int {
	if date == "" {
		return -1
	}
	d, err := time.ParseInLocation(DateLayout, date, time.Local)
	if err != nil {
		return -1
	}
	today, _ := time.ParseInLocation(DateLayout, s.Today(), time.Local)
	return int(today.Sub(d).Hours()+12) / 24 // +12h absorbs DST shifts
}

// Old reports whether a cartridge installed on date has passed warnMonths.
func (s *Store) Old(date string, warnMonths int) bool {
	d, err := time.ParseInLocation(DateLayout, date, time.Local)
	if err != nil {
		return false
	}
	return !s.now().Before(d.AddDate(0, warnMonths, 0))
}

func writeAtomic(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".supplies-*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	_, werr := tmp.Write(data)
	if werr == nil {
		werr = tmp.Sync()
	}
	if cerr := tmp.Close(); werr == nil {
		werr = cerr
	}
	if werr != nil {
		os.Remove(name)
		return werr
	}
	return os.Rename(name, path)
}
