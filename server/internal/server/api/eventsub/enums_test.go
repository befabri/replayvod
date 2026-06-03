package eventsub

import (
	"testing"

	"github.com/befabri/replayvod/server/internal/config"
)

// TestServerModeParity pins each wire-enum const to its config source. The
// consts are defined as aliases, so this fails loudly if a wire member is ever
// pointed at the wrong config value or replaced with a drifting literal. A
// coordinated rename of the config value itself propagates to the wire (config
// stays the source of truth) and is caught at the dashboard regeneration.
func TestServerModeParity(t *testing.T) {
	modes := map[ServerMode]string{
		ServerModeOff:    config.ServerModeOff,
		ServerModePoll:   config.ServerModePoll,
		ServerModeDirect: config.ServerModeDirect,
		ServerModeRelay:  config.ServerModeRelay,
	}
	for got, want := range modes {
		if string(got) != want {
			t.Errorf("ServerMode %q != config %q", got, want)
		}
	}
}
