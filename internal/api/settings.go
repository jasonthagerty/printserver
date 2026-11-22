package api

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/jasonthagerty/printserver/internal/settings"
)

// NextcloudView is the Nextcloud destination without its password.
type NextcloudView struct {
	URL         string `json:"url"`
	User        string `json:"user"`
	Folder      string `json:"folder"`
	PasswordSet bool   `json:"password_set"`
	Configured  bool   `json:"configured"`
}

// SettingsOutput is the Settings page.
type SettingsOutput struct {
	Body struct {
		RetentionDays int           `json:"retention_days"`
		Nextcloud     NextcloudView `json:"nextcloud"`
	}
}

// NextcloudInput edits the destination. The password is write-only: leave
// it out (or empty) to keep the saved one.
type NextcloudInput struct {
	URL           string `json:"url" maxLength:"500"`
	User          string `json:"user" maxLength:"200"`
	Folder        string `json:"folder" maxLength:"300"`
	Password      string `json:"password,omitempty" maxLength:"500" doc:"app password; empty keeps the saved one"`
	ClearPassword bool   `json:"clear_password,omitempty" doc:"forget the saved password"`
}

// SettingsInput replaces the settings.
type SettingsInput struct {
	Body struct {
		RetentionDays int            `json:"retention_days" minimum:"1" maximum:"3650"`
		Nextcloud     NextcloudInput `json:"nextcloud"`
	}
}

// TestNextcloudInput tests a destination, saved or about to be saved.
type TestNextcloudInput struct {
	Body struct {
		Nextcloud NextcloudInput `json:"nextcloud"`
	}
}

// TestOutput is the result of a connection test.
type TestOutput struct {
	Body struct {
		OK      bool   `json:"ok"`
		Message string `json:"message"`
	}
}

func registerSettings(api huma.API, deps Deps) {
	huma.Register(api, huma.Operation{
		OperationID: "get-settings",
		Method:      http.MethodGet,
		Path:        "/settings",
		Summary:     "Settings",
		Tags:        []string{"Settings"},
	}, func(ctx context.Context, _ *struct{}) (*SettingsOutput, error) {
		return settingsOutput(deps.Settings.Get()), nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "update-settings",
		Method:      http.MethodPut,
		Path:        "/settings",
		Summary:     "Change settings",
		Tags:        []string{"Settings"},
	}, func(ctx context.Context, in *SettingsInput) (*SettingsOutput, error) {
		next := deps.Settings.Get()
		next.RetentionDays = in.Body.RetentionDays
		next.Nextcloud = mergeNextcloud(next.Nextcloud, in.Body.Nextcloud)
		if err := deps.Settings.Update(next); err != nil {
			return nil, huma.Error422UnprocessableEntity(err.Error())
		}
		deps.Log.Info("settings changed", "retention_days", next.RetentionDays, "nextcloud", next.Nextcloud.Configured())
		return settingsOutput(deps.Settings.Get()), nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "test-nextcloud",
		Method:      http.MethodPost,
		Path:        "/settings/nextcloud/test",
		Summary:     "Test a Nextcloud connection",
		Description: "Tests the given values, using the saved password when none is given. Saves nothing.",
		Tags:        []string{"Settings"},
	}, func(ctx context.Context, in *TestNextcloudInput) (*TestOutput, error) {
		cfg := mergeNextcloud(deps.Settings.Get().Nextcloud, in.Body.Nextcloud)
		trial := settings.Settings{RetentionDays: 1, Nextcloud: cfg}
		out := &TestOutput{}
		if err := trial.Validate(); err != nil {
			out.Body.Message = err.Error()
			return out, nil
		}
		ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
		defer cancel()
		if err := deps.Nextcloud.Test(ctx, cfg); err != nil {
			out.Body.Message = err.Error()
			return out, nil
		}
		out.Body.OK, out.Body.Message = true, "Connected. Scans will go to /"+cfg.Folder+"."
		return out, nil
	})
}

func mergeNextcloud(cur settings.Nextcloud, in NextcloudInput) settings.Nextcloud {
	next := settings.Nextcloud{URL: in.URL, User: in.User, Folder: in.Folder, Password: cur.Password}
	if in.Password != "" {
		next.Password = in.Password
	}
	if in.ClearPassword {
		next.Password = ""
	}
	// A different server or user must not inherit the old password.
	if in.Password == "" && (normalizeURL(in.URL) != normalizeURL(cur.URL) || strings.TrimSpace(in.User) != strings.TrimSpace(cur.User)) {
		next.Password = ""
	}
	return next
}

func normalizeURL(s string) string {
	return strings.TrimRight(strings.TrimSpace(s), "/")
}

func settingsOutput(s settings.Settings) *SettingsOutput {
	out := &SettingsOutput{}
	out.Body.RetentionDays = s.RetentionDays
	n := s.Nextcloud
	out.Body.Nextcloud = NextcloudView{
		URL: n.URL, User: n.User, Folder: n.Folder,
		PasswordSet: n.Password != "", Configured: n.Configured(),
	}
	return out
}
