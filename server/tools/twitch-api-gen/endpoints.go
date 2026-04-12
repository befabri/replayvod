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

	// Stream markers — Twitch-authored segmentation for VOD post-processing.
	"get-stream-markers",
	"create-stream-marker",

	// Clips — per-broadcaster clip listings for the dashboard.
	"get-clips",

	// Broadcaster schedule — mirror Twitch's programmatic stream schedule.
	"get-channel-stream-schedule",
	"update-channel-stream-schedule",
	"create-channel-stream-schedule-segment",
	"update-channel-stream-schedule-segment",
	"delete-channel-stream-schedule-segment",
}
