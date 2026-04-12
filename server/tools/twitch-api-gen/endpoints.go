package main

// Endpoints to generate. Add IDs and re-run `task twitch-api-gen`.
var endpoints = []string{
	// Phase 3
	"get-users",
	"get-channel-information",
	"modify-channel-information",
	"get-followed-channels",
	"get-games",
	"get-top-games",

	// Phase 4
	"get-streams",
	"get-followed-streams",
	"get-videos",

	// Phase 5
	"create-eventsub-subscription",
	"get-eventsub-subscriptions",
	"delete-eventsub-subscription",
}
