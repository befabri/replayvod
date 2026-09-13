package server

import (
	"testing"

	"github.com/befabri/replayvod/server/internal/server/api"
)

func TestServerRequiresSharedRecordingServices(t *testing.T) {
	for _, recordings := range []*api.RecordingServices{nil, {}} {
		func() {
			defer func() {
				if got := recover(); got != "shared recording services required" {
					t.Errorf("missing safety wiring panic=%v", got)
				}
			}()
			NewServer(nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, recordings)
		}()
	}
}
