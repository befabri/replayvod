import { describe, expect, it } from "vitest";
import type { TimelineEvent } from "@/api/generated/trpc";
import {
	categoryTimelineEvents,
	eventStateKey,
	timelineEventOffsetSeconds,
	timelineEventsWithSpanFallback,
	timelineMarkerKind,
	timelineMarkerLabel,
	titleTimelineEvents,
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

describe("timeline dimension filters", () => {
	it("keeps only category rows and collapses consecutive duplicate categories", () => {
		const events: TimelineEvent[] = [
			event({ category: { id: "a", name: "A" } }),
			event({ category: { id: "a", name: "A again" } }),
			event({ title: { id: 1, name: "Title only" } }),
			event({ category: { id: "b", name: "B" } }),
			event({ category: { id: "a", name: "A return" } }),
		];

		expect(categoryTimelineEvents(events).map((e) => e.category.id)).toEqual([
			"a",
			"b",
			"a",
		]);
	});

	it("keeps only title rows and collapses consecutive duplicate titles", () => {
		const events: TimelineEvent[] = [
			event({ title: { id: 1, name: "Opening" } }),
			event({ title: { id: 2, name: "Opening" } }),
			event({ category: { id: "game", name: "Game" } }),
			event({ title: { id: 3, name: "Finale" } }),
		];

		expect(titleTimelineEvents(events).map((e) => e.title.name)).toEqual([
			"Opening",
			"Finale",
		]);
	});
});

describe("timeline marker helpers", () => {
	it("labels only changed fields and classifies mixed changes", () => {
		const row = event({
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
				event({
					category: { id: "game", name: "Game" },
					title: { id: 1, name: " Same title " },
				}),
			),
		).toBe("game:Same title");
	});
});

describe("timelineEventsWithSpanFallback", () => {
	it("returns server timeline rows when both dimensions are already present", () => {
		const events: TimelineEvent[] = [
			event({
				occurred_at: "2026-01-01T00:00:30Z",
				media_offset_seconds: 30,
				category: { id: "game", name: "Game" },
			}),
			event({
				occurred_at: "2026-01-01T00:01:00Z",
				media_offset_seconds: 60,
				title: { id: 2, name: "Second" },
			}),
		];

		expect(
			timelineEventsWithSpanFallback(
				events,
				[
					{
						id: 1,
						name: "Legacy title",
						started_at: "2026-01-01T00:00:00Z",
						duration_seconds: 30,
					},
				],
				[
					{
						id: "legacy",
						name: "Legacy category",
						started_at: "2026-01-01T00:00:00Z",
						duration_seconds: 30,
					},
				],
			),
		).toEqual(events);
	});

	it("builds merged fallback rows from legacy title and category spans", () => {
		const events = timelineEventsWithSpanFallback(
			[],
			[
				{
					id: 1,
					name: "Opening",
					started_at: "2026-01-01T00:00:00Z",
					duration_seconds: 60,
				},
				{
					id: 2,
					name: "Second",
					started_at: "2026-01-01T00:01:00Z",
					duration_seconds: 30,
				},
			],
			[
				{
					id: "coworking",
					name: "Co-working & Studying",
					box_art_url: null,
					started_at: "2026-01-01T00:00:00Z",
					duration_seconds: 90,
				},
			],
		);

		expect(events).toEqual([
			{
				occurred_at: "2026-01-01T00:00:00Z",
				title: { id: 1, name: "Opening" },
				category: {
					id: "coworking",
					name: "Co-working & Studying",
					box_art_url: undefined,
				},
			},
			{
				occurred_at: "2026-01-01T00:01:00Z",
				title: { id: 2, name: "Second" },
			},
		]);
	});

	it("fills a missing timeline dimension from legacy spans", () => {
		const events = timelineEventsWithSpanFallback(
			[
				event({
					occurred_at: "2026-01-01T00:01:00Z",
					media_offset_seconds: 59.8,
					title: { id: 2, name: "Second" },
				}),
			],
			[
				{
					id: 1,
					name: "Opening",
					started_at: "2026-01-01T00:00:00Z",
					duration_seconds: 60,
				},
			],
			[
				{
					id: "game",
					name: "Game",
					started_at: "2026-01-01T00:00:00Z",
					duration_seconds: 90,
				},
			],
		);

		expect(events).toEqual([
			{
				occurred_at: "2026-01-01T00:00:00Z",
				category: { id: "game", name: "Game", box_art_url: undefined },
			},
			{
				occurred_at: "2026-01-01T00:01:00Z",
				media_offset_seconds: 59.8,
				title: { id: 2, name: "Second" },
			},
		]);
	});
});

function event(partial: Partial<TimelineEvent>): TimelineEvent {
	return {
		occurred_at: "2026-01-01T00:00:00Z",
		...partial,
	};
}
