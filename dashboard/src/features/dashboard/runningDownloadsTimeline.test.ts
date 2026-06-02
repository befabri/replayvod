import { describe, expect, it } from "vitest";
import type {
	ActiveDownloadResponse,
	TimelineEvent,
	VideoResponse,
} from "@/api/generated/trpc";
import {
	clampMetadataMarkers,
	contentSegments,
	type MetadataMarker,
	metadataMarkers,
	recordingElapsedSeconds,
} from "./runningDownloadsTimeline";

describe("running download timeline helpers", () => {
	it("uses exact live media offset for elapsed time", () => {
		expect(recordingElapsedSeconds(row({ media_offset_seconds: 95 }))).toBe(95);
	});

	it("falls back to finalized part durations when media offset is unavailable", () => {
		expect(
			recordingElapsedSeconds(
				row({
					media_offset_seconds: undefined,
					video: video({
						parts: [
							part({ part_index: 2, duration_seconds: 15 }),
							part({ part_index: 1, duration_seconds: 30 }),
						],
					}),
				}),
			),
		).toBe(45);
	});

	it("places metadata markers by media offset and filters future events", () => {
		const markers = clampMetadataMarkers(
			metadataMarkers(
				[
					event({
						media_offset_seconds: 10,
						title: { id: 1, name: "Opening" },
					}),
					event({
						media_offset_seconds: 20,
						category: { id: "game", name: "Game" },
						title: { id: 2, name: "Boss" },
					}),
					event({
						media_offset_seconds: 200,
						category: { id: "late", name: "Late" },
					}),
				],
				"2026-01-01T00:00:00Z",
			),
			60,
		);

		expect(markers.map((marker) => marker.kind)).toEqual(["title", "mixed"]);
		expect(markers.map((marker) => marker.offsetSeconds)).toEqual([10, 20]);
		expect(markers[1]?.label).toBe("Game - Boss");
	});

	it("clamps a wall-clock-fallback marker to the live edge instead of dropping it", () => {
		// No media_offset_seconds → wall-clock fallback. The wall clock (120s since
		// start) runs ahead of the media-clock scale (60s) because of gaps/startup
		// lag, but the marker is a valid recent change — it must pin to the live
		// edge, not vanish.
		const markers = clampMetadataMarkers(
			metadataMarkers(
				[
					event({
						occurred_at: "2026-01-01T00:02:00Z", // 120s after the anchor
						title: { id: 7, name: "Recent change" },
					}),
				],
				"2026-01-01T00:00:00Z",
			),
			60,
		);

		expect(markers).toHaveLength(1);
		expect(markers[0]?.offsetSeconds).toBe(60);
		expect(markers[0]?.label).toBe("Recent change");
	});

	it("marks only changed metadata fields when timeline rows include current state", () => {
		const markers = metadataMarkers(
			[
				event({
					media_offset_seconds: 10,
					category: { id: "game-a", name: "Game A" },
					title: { id: 1, name: "Same title" },
				}),
				event({
					media_offset_seconds: 20,
					category: { id: "game-b", name: "Game B" },
					title: { id: 1, name: "Same title" },
				}),
			],
			"2026-01-01T00:00:00Z",
		);

		expect(markers.map((marker) => marker.kind)).toEqual(["mixed", "category"]);
		expect(markers.map((marker) => marker.label)).toEqual([
			"Game A - Same title",
			"Game B",
		]);
	});
});

describe("contentSegments", () => {
	it("returns one full-span segment when there are no changes", () => {
		expect(contentSegments([], 90)).toEqual([
			{ key: "seg-0", startSeconds: 0, endSeconds: 90 },
		]);
	});

	it("splits at a title-only change, keeping the active category", () => {
		const markers: MetadataMarker[] = [
			metaMarker(0, { category: { id: "c1", name: "Rust" }, title: "early" }),
			metaMarker(30, { title: "boss fight" }),
		];
		expect(contentSegments(markers, 60)).toEqual([
			{
				key: "k0:0",
				startSeconds: 0,
				endSeconds: 30,
				category: { id: "c1", name: "Rust" },
				title: { name: "early" },
			},
			{
				key: "k30:1",
				startSeconds: 30,
				endSeconds: 60,
				category: { id: "c1", name: "Rust" },
				title: { name: "boss fight" },
			},
		]);
	});

	it("prepends a neutral band before the first change", () => {
		const markers: MetadataMarker[] = [
			metaMarker(20, { category: { id: "c1", name: "Rust" } }),
		];
		expect(contentSegments(markers, 60)).toEqual([
			{ key: "seg-init", startSeconds: 0, endSeconds: 20 },
			{
				key: "k20:0",
				startSeconds: 20,
				endSeconds: 60,
				category: { id: "c1", name: "Rust" },
			},
		]);
	});

	it("sorts markers by offset before segmenting", () => {
		const markers: MetadataMarker[] = [
			metaMarker(40, { category: { id: "c2", name: "Two" } }),
			metaMarker(0, { category: { id: "c1", name: "One" } }),
		];
		expect(contentSegments(markers, 60)).toEqual([
			{
				key: "k0:0",
				startSeconds: 0,
				endSeconds: 40,
				category: { id: "c1", name: "One" },
			},
			{
				key: "k40:1",
				startSeconds: 40,
				endSeconds: 60,
				category: { id: "c2", name: "Two" },
			},
		]);
	});
});

function metaMarker(
	offsetSeconds: number,
	opts: {
		category?: { id: string; name: string };
		title?: string;
	},
): MetadataMarker {
	return {
		key: `k${offsetSeconds}`,
		offsetSeconds,
		label: "",
		kind: opts.category ? "category" : "title",
		category: opts.category,
		title: opts.title ? { name: opts.title } : undefined,
	};
}

function row(
	partial: Partial<ActiveDownloadResponse> = {},
): ActiveDownloadResponse {
	return {
		video: video(),
		part_index: 1,
		stage: "segments",
		bytes_written: 100,
		segments_done: 10,
		segments_gaps: 0,
		segments_ad_gaps: 0,
		segments_total: -1,
		percent: -1,
		...partial,
	};
}

function video(partial: Partial<VideoResponse> = {}): VideoResponse {
	return {
		id: 1,
		job_id: "job",
		filename: "vod",
		display_name: "Streamer",
		title: "Title",
		status: "RUNNING",
		completion_kind: "complete",
		truncated: false,
		quality: "1080p60",
		broadcaster_id: "b1",
		viewer_count: 0,
		language: "fr",
		start_download_at: "2026-01-01T00:00:00Z",
		...partial,
	};
}

function part(partial: Partial<NonNullable<VideoResponse["parts"]>[number]>) {
	const partIndex = partial.part_index ?? 1;
	return {
		id: partial.id ?? partIndex,
		part_index: partIndex,
		filename: "vod-part01.mp4",
		quality: "1080p60",
		codec: "avc1",
		segment_format: "ts",
		duration_seconds: 10,
		size_bytes: 100,
		start_media_seq: 1,
		...partial,
	};
}

function event(partial: Partial<TimelineEvent>): TimelineEvent {
	return {
		occurred_at: "2026-01-01T00:00:00Z",
		...partial,
	};
}
