package api

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/jasonthagerty/printserver/internal/canon"
	"github.com/jasonthagerty/printserver/internal/cups"
	"github.com/jasonthagerty/printserver/internal/supplies"
)

// CartridgeView is one cartridge with its age worked out.
type CartridgeView struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Model       string `json:"model"`
	InstalledOn string `json:"installed_on" doc:"YYYY-MM-DD; empty when not recorded"`
	AgeDays     int    `json:"age_days" doc:"-1 when the install date is not recorded"`
	Old         bool   `json:"old" doc:"older than warn_months: the ink may be drying out"`
	Level       int    `json:"level" doc:"ink percent from the last reading; -1 when unknown"`
	Low         bool   `json:"low" doc:"at or below the printer's own low-ink level"`
	Color       string `json:"color" doc:"#RRGGBB as the printer reports it, possibly several; empty when unknown"`
}

// SuppliesOutput is cartridge ages and printer use.
type SuppliesOutput struct {
	Body struct {
		Cartridges  []CartridgeView `json:"cartridges"`
		WarnMonths  int             `json:"warn_months"`
		NudgeDays   int             `json:"nudge_days" doc:"0: no test page reminder"`
		LastPrinted *time.Time      `json:"last_printed" doc:"null until a print has been recorded"`
		IdleDays    int             `json:"idle_days" doc:"days since the last print; -1 when unknown"`
		Nudge       bool            `json:"nudge" doc:"suggest a test page: no print for nudge_days"`
		InkChecked  *time.Time      `json:"ink_checked" doc:"when ink levels were last read; null when never"`
	}
}

// CartridgeDateInput sets a cartridge's install date.
type CartridgeDateInput struct {
	ID   string `path:"id" enum:"black,color"`
	Body struct {
		InstalledOn string `json:"installed_on" pattern:"^(|[0-9]{4}-[0-9]{2}-[0-9]{2})$" doc:"YYYY-MM-DD, or empty to clear"`
	}
}

// CartridgeIDInput selects a cartridge.
type CartridgeIDInput struct {
	ID string `path:"id" enum:"black,color"`
}

// MaintenanceInput runs a print head maintenance job.
type MaintenanceInput struct {
	Body struct {
		Action string `json:"action" enum:"nozzle-check,cleaning" doc:"nozzle-check prints Canon's check pattern (one sheet); cleaning cleans the print head (uses some ink)"`
	}
}

// ThresholdsInput changes the warning age and reminder.
type ThresholdsInput struct {
	Body struct {
		WarnMonths int `json:"warn_months" minimum:"1" maximum:"60"`
		NudgeDays  int `json:"nudge_days" minimum:"0" maximum:"365"`
	}
}

func registerSupplies(api huma.API, deps Deps) {
	huma.Register(api, huma.Operation{
		OperationID: "get-supplies",
		Method:      http.MethodGet,
		Path:        "/supplies",
		Summary:     "Cartridge ages and last print",
		Tags:        []string{"Supplies"},
	}, func(ctx context.Context, _ *struct{}) (*SuppliesOutput, error) {
		return suppliesOutput(deps.Supplies, deps.Supplies.Get()), nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "set-cartridge-date",
		Method:      http.MethodPut,
		Path:        "/supplies/cartridges/{id}",
		Summary:     "Set when a cartridge was installed",
		Tags:        []string{"Supplies"},
	}, func(ctx context.Context, in *CartridgeDateInput) (*SuppliesOutput, error) {
		st, err := deps.Supplies.SetInstalled(in.ID, in.Body.InstalledOn)
		if err != nil {
			return nil, suppliesError(err)
		}
		return suppliesOutput(deps.Supplies, st), nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "replace-cartridge",
		Method:      http.MethodPost,
		Path:        "/supplies/cartridges/{id}/replaced",
		Summary:     "Record that a cartridge was replaced today",
		Tags:        []string{"Supplies"},
	}, func(ctx context.Context, in *CartridgeIDInput) (*SuppliesOutput, error) {
		st, err := deps.Supplies.SetInstalled(in.ID, deps.Supplies.Today())
		if err != nil {
			return nil, suppliesError(err)
		}
		deps.Log.Info("cartridge replaced", "cartridge", in.ID)
		return suppliesOutput(deps.Supplies, st), nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "set-supply-thresholds",
		Method:      http.MethodPut,
		Path:        "/supplies/thresholds",
		Summary:     "Change the cartridge warning age and test page reminder",
		Tags:        []string{"Supplies"},
	}, func(ctx context.Context, in *ThresholdsInput) (*SuppliesOutput, error) {
		st, err := deps.Supplies.SetThresholds(in.Body.WarnMonths, in.Body.NudgeDays)
		if err != nil {
			return nil, suppliesError(err)
		}
		return suppliesOutput(deps.Supplies, st), nil
	})

	huma.Register(api, huma.Operation{
		OperationID:   "print-test-page",
		Method:        http.MethodPost,
		Path:          "/supplies/test-page",
		Summary:       "Print a color test page",
		Description:   "Uses every ink, which keeps the nozzles from drying, and shows whether one is clogged.",
		Tags:          []string{"Supplies"},
		DefaultStatus: http.StatusCreated,
	}, func(ctx context.Context, _ *struct{}) (*PrintOutput, error) {
		id, err := deps.CUPS.Print(ctx, bytes.NewReader(supplies.TestPage(time.Now())), cups.PrintOptions{
			Name: "Test page", Format: "application/pdf", ColorMode: "color",
		})
		if err != nil {
			return nil, cupsError(deps, err)
		}
		out := &PrintOutput{}
		out.Body.ID = id
		return out, nil
	})
}

func registerMaintenance(api huma.API, deps Deps) {
	huma.Register(api, huma.Operation{
		OperationID:   "run-maintenance",
		Method:        http.MethodPost,
		Path:          "/supplies/maintenance",
		Summary:       "Nozzle check or print head cleaning",
		Description:   "Sends Canon's maintenance command to the printer as a raw job. Canon printers only.",
		Tags:          []string{"Supplies"},
		DefaultStatus: http.StatusCreated,
	}, func(ctx context.Context, in *MaintenanceInput) (*PrintOutput, error) {
		p, err := deps.CUPS.Printer(ctx)
		if err != nil {
			return nil, cupsError(deps, err)
		}
		if !strings.Contains(strings.ToLower(p.MakeAndModel), "canon") {
			return nil, huma.Error409Conflict("maintenance commands are only known for Canon printers")
		}
		job, err := canon.Job(canon.Action(in.Body.Action), time.Now())
		if err != nil {
			return nil, huma.Error422UnprocessableEntity(err.Error())
		}
		name := "Nozzle check"
		if in.Body.Action == string(canon.Cleaning) {
			name = "Print head cleaning"
		}
		id, err := deps.CUPS.Print(ctx, bytes.NewReader(job), cups.PrintOptions{
			Name: name, Format: "application/vnd.cups-raw",
		})
		if err != nil {
			return nil, cupsError(deps, err)
		}
		deps.Log.Info("maintenance job", "action", in.Body.Action, "job", id)
		out := &PrintOutput{}
		out.Body.ID = id
		return out, nil
	})
}

func suppliesOutput(s *supplies.Store, st supplies.State) *SuppliesOutput {
	out := &SuppliesOutput{}
	b := &out.Body
	b.WarnMonths, b.NudgeDays, b.LastPrinted = st.WarnMonths, st.NudgeDays, st.LastPrinted
	b.Cartridges = make([]CartridgeView, 0, len(st.Cartridges))
	for _, c := range st.Cartridges {
		v := CartridgeView{
			ID: c.ID, Name: c.Name, Model: c.Model, InstalledOn: c.InstalledOn,
			AgeDays: s.AgeDays(c.InstalledOn),
			Old:     c.InstalledOn != "" && s.Old(c.InstalledOn, st.WarnMonths),
			Level:   -1,
		}
		if l, ok := st.Ink.Level(c.ID); ok {
			v.Level, v.Color = l.Level, l.Color
			v.Low = l.Level >= 0 && l.Low >= 0 && l.Level <= l.Low
		}
		b.Cartridges = append(b.Cartridges, v)
	}
	if st.Ink != nil {
		t := st.Ink.CheckedAt
		b.InkChecked = &t
	}
	b.IdleDays = -1
	if st.LastPrinted != nil {
		b.IdleDays = s.AgeDays(st.LastPrinted.Local().Format(supplies.DateLayout))
		b.Nudge = st.NudgeDays > 0 && b.IdleDays >= st.NudgeDays
	}
	return out
}

func suppliesError(err error) error {
	switch {
	case errors.Is(err, supplies.ErrNotFound):
		return huma.Error404NotFound(err.Error())
	case errors.Is(err, supplies.ErrInvalid):
		return huma.Error422UnprocessableEntity(err.Error())
	}
	return huma.Error500InternalServerError("could not save; check the server log")
}
