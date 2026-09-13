package api

import (
	"net/http"
	"testing"

	settingsapi "github.com/befabri/replayvod/server/internal/server/api/settings"
)

func TestPlaybackSettingsValidationAndIsolationOverHTTP(t *testing.T) {
	h := newPermissionHarness(t)
	path := "/trpc/settings.updatePlayback"
	valid := `{"resume_min_seconds":50,"resume_end_margin_seconds":10,"resume_end_margin_percent":5}`
	permissionResponse(t, h, http.MethodPost, path, valid, nil, http.StatusUnauthorized)
	for _, body := range []string{
		`{}`,
		`{"resume_min_seconds":50,"resume_end_margin_seconds":10,"resume_end_margin_percent":5,"user_id":"perm-admin-1"}`,
		`{"resume_min_seconds":0,"resume_end_margin_seconds":10,"resume_end_margin_percent":5}`,
		`{"resume_min_seconds":601,"resume_end_margin_seconds":10,"resume_end_margin_percent":5}`,
		`{"resume_min_seconds":5,"resume_end_margin_seconds":0,"resume_end_margin_percent":5}`,
		`{"resume_min_seconds":5,"resume_end_margin_seconds":601,"resume_end_margin_percent":5}`,
		`{"resume_min_seconds":5,"resume_end_margin_seconds":10,"resume_end_margin_percent":51}`,
		`{"resume_min_seconds":5.5,"resume_end_margin_seconds":10,"resume_end_margin_percent":5}`,
	} {
		permissionResponse(t, h, http.MethodPost, path, body, h.viewer, http.StatusBadRequest)
	}
	saved := decodePermissionData[settingsapi.SettingsResponse](t, permissionResponse(t, h, http.MethodPost, path, valid, h.viewer, http.StatusOK))
	if saved.UserID != "perm-viewer-1" || saved.Playback.ResumeMinSeconds != 50 || saved.Playback.ResumeEndMarginSeconds != 10 {
		t.Fatalf("saved wrong viewer or policy: %+v", saved)
	}
	loaded := decodePermissionData[settingsapi.SettingsResponse](t, permissionResponse(t, h, http.MethodGet, "/trpc/settings.get", "", h.viewer, http.StatusOK))
	if loaded.Playback != saved.Playback {
		t.Fatalf("preferences did not persist: %+v", loaded)
	}
	other := decodePermissionData[settingsapi.SettingsResponse](t, permissionResponse(t, h, http.MethodGet, "/trpc/settings.get", "", h.admin, http.StatusOK))
	if other.Playback.ResumeMinSeconds != 5 {
		t.Fatalf("preferences leaked to another viewer: %+v", other)
	}
}
