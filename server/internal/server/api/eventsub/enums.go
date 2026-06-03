package eventsub

import "github.com/befabri/replayvod/server/internal/config"

// ServerMode is the wire enum for the EventSub delivery mode, surfaced on the
// config response DTOs so trpcgo emits an "off" | "poll" | "direct" | "relay"
// union instead of a bare string. The values alias the config.ServerMode*
// constants, which remain the source of truth; the saved/active config keeps
// plain strings and is converted at the response boundary. TestServerModeParity
// guards against the alias drifting.
type ServerMode string

const (
	ServerModeOff    ServerMode = config.ServerModeOff
	ServerModePoll   ServerMode = config.ServerModePoll
	ServerModeDirect ServerMode = config.ServerModeDirect
	ServerModeRelay  ServerMode = config.ServerModeRelay
)
