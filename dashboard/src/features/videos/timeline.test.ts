import { describe, expect, it } from "vitest";
import { makeTimelineEvent } from "@/test/fixtures";
import {
	eventStateKey,
	timelineEventOffsetSeconds,
	timelineMarkerKind,
	timelineMarkerLabel,
} from "./timeline";

describe("timelineEventOffsetSeconds", () => {
	it("prefers exact media offsets over wall-clock deltas", () => {
		const event = {
			occurred_at: "2026-01-01T01:00:00Z",
			media_offset_seconds: 125.4,
		};

		expect(timelineEventOffsetSeconds(event, "2026-01-01T00:00:00Z")).toBe(125);
	});

	it("falls back to wall-clock deltas when the exact offset is absent", () => {
		const event = {
			occurred_at: "2026-01-01T00:02:05Z",
		};

		expect(timelineEventOffsetSeconds(event, "2026-01-01T00:00:00Z")).toBe(125);
	});

	it("clamps negative exact offsets to zero", () => {
		const event = {
			occurred_at: "2026-01-01T00:02:05Z",
			media_offset_seconds: -5,
		};

		expect(timelineEventOffsetSeconds(event, "2026-01-01T00:00:00Z")).toBe(0);
	});

	// Regression: an unparseable timestamp must not produce NaN — that slips
	// past `< 0` / clamp() guards and renders a marker at left: NaN%.
	it("returns a negative sentinel (never NaN) for an unparseable anchor", () => {
		const event = { occurred_at: "2026-01-01T00:02:05Z" };
		const offset = timelineEventOffsetSeconds(event, "not-a-date");
		expect(Number.isNaN(offset)).toBe(false);
		expect(offset).toBeLessThan(0);
	});

	it("returns a negative sentinel for an unparseable occurred_at", () => {
		const event = { occurred_at: "garbage" };
		const offset = timelineEventOffsetSeconds(event, "2026-01-01T00:00:00Z");
		expect(Number.isNaN(offset)).toBe(false);
		expect(offset).toBeLessThan(0);
	});
});

describe("timeline marker helpers", () => {
	it("labels only changed fields and classifies mixed changes", () => {
		const row = makeTimelineEvent({
			category: { id: "game", name: " Game " },
			title: { id: 1, name: " Boss run " },
		});

		expect(timelineMarkerLabel(row, ["category"])).toBe("Game");
		expect(timelineMarkerLabel(row, ["category", "title"])).toBe(
			"Game - Boss run",
		);
		expect(timelineMarkerKind(["category", "title"])).toBe("mixed");
	});

	it("uses the same trimmed event state key for dedup callers", () => {
		expect(
			eventStateKey(
				makeTimelineEvent({
					category: { id: "game", name: "Game" },
					title: { id: 1, name: " Same title " },
				}),
			),
		).toBe("game:Same title");
	});
});
