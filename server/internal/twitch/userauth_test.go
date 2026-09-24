package twitch

import (
	"errors"
	"net/http"
	"testing"
)

func TestIsUserAuthError(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{name: "401", err: &HelixError{Status: http.StatusUnauthorized}, want: true},
		{name: "403", err: &HelixError{Status: http.StatusForbidden}, want: true},
		{name: "500", err: &HelixError{Status: http.StatusInternalServerError}, want: false},
		{name: "wrapped 401", err: errors.New("wrap: " + (&HelixError{Status: http.StatusUnauthorized}).Error()), want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := IsUserAuthError(tt.err)
			if got != tt.want {
				t.Fatalf("IsUserAuthError(%v) = %v, want %v", tt.err, got, tt.want)
			}
		})
	}

	wrapped := errors.Join(errors.New("context"), &HelixError{Status: http.StatusUnauthorized})
	if !IsUserAuthError(wrapped) {
		t.Fatal("joined helix 401 should be treated as auth error")
	}
}
