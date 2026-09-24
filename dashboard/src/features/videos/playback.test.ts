import { describe, expect, it, vi } from "vitest";
import type { VideoResponse } from "@/api/generated/trpc";
import {
	makeTimelineEvent,
	makeVideo,
	makeVideoPart,
	VIDEO_REMOVAL_STATES,
	VIDEO_STATES,
} from "@/test/fixtures";
import {
	buildPlaylistParts,
	buildRecordingMarkers,
	buildRecordingPlaylist,
	chapterCuesForPart,
	chapterCuesForRecording,
	continuousSourceForVideo,
	findPartForOffset,
	isPlayableVideo,
	probeMediaSource,
} from "./playback";

describe("buildPlaylistParts", () => {
	it("uses authenticated per-part stream URLs and cumulative offsets", () => {
		const parts = buildPlaylistParts(
			video({
				parts: [
					part({
						part_index: 2,
						filename: "vod-part02.mp4",
						duration_seconds: 30,
					}),
					part({
						part_index: 1,
						filename: "vod-part01.mp4",
						duration_seconds: 10,
					}),
				],
			}),
			"https://api.example",
		);

		expect(
			parts.map((p) => [p.partIndex, p.startSeconds, p.endSeconds]),
		).toEqual([
			[1, 0, 10],
			[2, 10, 40],
		]);
		expect(parts[1].src).toBe(
			"https://api.example/api/v1/videos/65/parts/2/stream",
		);
	});

	it("requires part references instead of guessing a historical media URL", () => {
		expect(buildPlaylistParts(video({ parts: undefined }), "")).toEqual([]);
	});

	it("uses the server audio-only verdict over part filename inference", () => {
		const audioParts = buildPlaylistParts(
			video({
				is_audio_only: true,
				parts: [part({ filename: "vod-part01.mp4" })],
			}),
			"",
		);
		const videoParts = buildPlaylistParts(
			video({
				is_audio_only: false,
				parts: [part({ filename: "vod-part01.m4a" })],
			}),
			"",
		);

		expect(audioParts[0].mimeType).toBe("audio/mp4");
		expect(videoParts[0].mimeType).toBe("video/mp4");
	});
});

describe("findPartForOffset", () => {
	it("maps global offsets to part-local seek targets", () => {
		const playlist = buildRecordingPlaylist(
			video({
				parts: [
					part({ part_index: 1, duration_seconds: 10 }),
					part({ part_index: 2, duration_seconds: 30 }),
				],
			}),
			undefined,
		);

		expect(findPartForOffset(playlist.parts, 25)).toMatchObject({
			part: { partIndex: 2 },
			localSeconds: 15,
			globalSeconds: 25,
		});
	});

	it("moves exact boundaries to the next part", () => {
		const playlist = buildRecordingPlaylist(
			video({
				parts: [
					part({ part_index: 1, duration_seconds: 10 }),
					part({ part_index: 2, duration_seconds: 30 }),
				],
			}),
			undefined,
		);

		expect(findPartForOffset(playlist.parts, 10)).toMatchObject({
			part: { partIndex: 2 },
			localSeconds: 0,
		});
	});

	it("clamps a seek to the caller-supplied total when it differs from summed parts", () => {
		const parts = buildPlaylistParts(
			video({
				parts: [
					part({ part_index: 1, duration_seconds: 60 }),
					part({ part_index: 2, duration_seconds: 60 }),
				],
			}),
			"",
		);

		// A muxed file shorter than the 120s EXTINF sum: an out-of-range seek
		// clamps to the supplied total (90) and part-local time stays real.
		expect(findPartForOffset(parts, 999, 90)).toMatchObject({
			part: { partIndex: 2 },
			localSeconds: 30,
			globalSeconds: 90,
		});
	});
});

describe("continuous playback source", () => {
	it("uses client-side part sequencing for multipart videos until a ready continuous artifact is exposed", () => {
		const row = video({
			parts: [
				part({ part_index: 1, filename: "vod-part01.mp4", fps: 60 }),
				part({ part_index: 2, filename: "vod-part02.mp4", fps: 60 }),
			],
		});
		const parts = buildPlaylistParts(row, "https://api.example");

		expect(
			continuousSourceForVideo(row, parts, "https://api.example"),
		).toBeNull();
	});

	it("uses the playback artifact stream when video.getById reports it ready", () => {
		const row = video({
			playback_artifact: {
				status: "ready",
				mime_type: "video/mp4",
				updated_at: "2026-01-01T00:00:00Z",
			},
			parts: [
				part({ part_index: 1, filename: "vod-part01.mp4", fps: 60 }),
				part({ part_index: 2, filename: "vod-part02.mp4", fps: 60 }),
			],
		});
		const parts = buildPlaylistParts(row, "https://api.example");

		expect(continuousSourceForVideo(row, parts, "https://api.example")).toEqual(
			{
				src: "https://api.example/api/v1/videos/65/playback/stream",
				mimeType: "video/mp4",
				durationSeconds: null,
			},
		);
	});

	it("uses the server audio-only verdict over playback artifact MIME", () => {
		const row = video({
			is_audio_only: true,
			playback_artifact: {
				status: "ready",
				mime_type: "video/mp4",
				updated_at: "2026-01-01T00:00:00Z",
			},
			parts: [part({ part_index: 1, filename: "vod-part01.mp4" })],
		});
		const parts = buildPlaylistParts(row, "");

		expect(continuousSourceForVideo(row, parts, "")?.mimeType).toBe(
			"audio/mp4",
		);
	});

	it("keeps the timeline on summed parts and carries the muxed duration for scaling", () => {
		const row = video({
			playback_artifact: {
				status: "ready",
				mime_type: "video/mp4",
				duration_seconds: 118, // probed muxed total, != 60 + 60
				updated_at: "2026-01-01T00:00:00Z",
			},
			parts: [
				part({ part_index: 1, duration_seconds: 60 }),
				part({ part_index: 2, duration_seconds: 60 }),
			],
		});

		const playlist = buildRecordingPlaylist(row, undefined, "");

		// One basis for the whole UI: the recording timeline is the summed parts
		// (120). The muxed file's probed duration (118) rides on the source so the
		// player reconciles its own clock without handing the UI a second basis.
		expect(playlist.totalDurationSeconds).toBe(120);
		expect(playlist.continuousSource?.durationSeconds).toBe(118);
	});

	it("falls back to client-side part sequencing for incompatible multipart videos", () => {
		const row = video({
			parts: [
				part({ part_index: 1, filename: "vod-part01.mp4", quality: "1080p60" }),
				part({ part_index: 2, filename: "vod-part02.mp4", quality: "720p60" }),
			],
		});
		const parts = buildPlaylistParts(row, "");

		expect(continuousSourceForVideo(row, parts, "")).toBeNull();
	});

	it("keeps modern single-part videos on the authenticated part stream", () => {
		const row = video({
			parts: [part({ part_index: 1, filename: "vod-part01.mp4" })],
		});
		const parts = buildPlaylistParts(row, "");

		expect(continuousSourceForVideo(row, parts, "")).toEqual({
			src: "/api/v1/videos/65/parts/1/stream",
			mimeType: "video/mp4",
			durationSeconds: 60,
		});
	});
});

describe("recording markers", () => {
	it("uses exact media offsets for title/category markers", () => {
		const markers = buildRecordingMarkers(
			[
				makeTimelineEvent({
					media_offset_seconds: 75,
					category: { id: "game", name: "Game" },
					title: { id: 1, name: "Boss run" },
				}),
			],
			"2026-01-01T00:00:00Z",
			120,
		);

		expect(markers).toMatchObject([
			{
				offsetSeconds: 75,
				label: "Game - Boss run",
				kind: "mixed",
				changes: ["category", "title"],
			},
		]);
	});

	it("labels only the field that changed when timeline rows include current state", () => {
		const markers = buildRecordingMarkers(
			[
				makeTimelineEvent({
					media_offset_seconds: 10,
					category: { id: "game-a", name: "Game A" },
					title: { id: 1, name: "Same title" },
				}),
				makeTimelineEvent({
					media_offset_seconds: 20,
					category: { id: "game-b", name: "Game B" },
					title: { id: 1, name: "Same title" },
				}),
				makeTimelineEvent({
					media_offset_seconds: 30,
					category: { id: "game-b", name: "Game B" },
					title: { id: 2, name: "New title" },
				}),
			],
			"2026-01-01T00:00:00Z",
			120,
		);

		expect(markers.map((marker) => marker.kind)).toEqual([
			"mixed",
			"category",
			"title",
		]);
		expect(markers.map((marker) => marker.label)).toEqual([
			"Game A - Same title",
			"Game B",
			"New title",
		]);
		expect(markers.map((marker) => marker.changes)).toEqual([
			["category", "title"],
			["category"],
			["title"],
		]);
	});

	it("builds part-local chapter cues from global markers", () => {
		const playlist = buildRecordingPlaylist(
			video({
				parts: [
					part({ part_index: 1, duration_seconds: 60 }),
					part({ part_index: 2, duration_seconds: 60 }),
				],
			}),
			[
				makeTimelineEvent({
					media_offset_seconds: 20,
					title: { id: 1, name: "Intro" },
				}),
				makeTimelineEvent({
					media_offset_seconds: 70,
					title: { id: 2, name: "Second" },
				}),
			],
		);

		expect(chapterCuesForPart(playlist.markers, playlist.parts[1])).toEqual([
			{ startTime: 0, endTime: 10, text: "Intro" },
			{ startTime: 10, endTime: 60, text: "Second" },
		]);
	});

	it("builds recording-wide chapter cues for the continuous source", () => {
		const playlist = buildRecordingPlaylist(
			video({
				parts: [
					part({ part_index: 1, duration_seconds: 60 }),
					part({ part_index: 2, duration_seconds: 60 }),
				],
			}),
			[
				makeTimelineEvent({
					media_offset_seconds: 20,
					title: { id: 1, name: "Intro" },
				}),
				makeTimelineEvent({
					media_offset_seconds: 70,
					title: { id: 2, name: "Second" },
				}),
			],
		);

		expect(
			chapterCuesForRecording(
				playlist.markers,
				playlist.totalDurationSeconds,
				playlist.title,
			),
		).toEqual([
			{ startTime: 0, endTime: 20, text: "Opening" },
			{ startTime: 20, endTime: 70, text: "Intro" },
			{ startTime: 70, endTime: 120, text: "Second" },
		]);
	});
});

function video(partial: Partial<VideoResponse>): VideoResponse {
	return makeVideo(64, {
		title: "Opening",
		is_audio_only: false,
		duration_seconds: 120,
		start_download_at: "2026-01-01T00:00:00Z",
		...partial,
	});
}

function part(
	partial: Partial<NonNullable<VideoResponse["parts"]>[number]> = {},
) {
	return makeVideoPart({ duration_seconds: 60, ...partial });
}

describe("probeMediaSource", () => {
	const src = "https://api.example/api/v1/videos/7/parts/1/stream";
	const answering = (status: number) =>
		vi.fn(
			async () => ({ ok: status >= 200 && status < 300, status }) as Response,
		);

	it("sends a credentialed HEAD so the status comes from the stream route itself", async () => {
		const fetchImpl = answering(200);

		await probeMediaSource(src, fetchImpl);

		expect(fetchImpl).toHaveBeenCalledWith(src, {
			method: "HEAD",
			credentials: "include",
			cache: "no-store",
			signal: expect.any(AbortSignal),
		});
	});

	it.each([
		[200, "ok"],
		[206, "ok"],
		[404, "gone"],
		[410, "removed"],
		[401, "failed"],
		[503, "failed"],
	] as const)("maps status %d to %s", async (status, expected) => {
		expect(await probeMediaSource(src, answering(status))).toBe(expected);
	});

	it("reports a rejected fetch as a transient failure, never as gone", async () => {
		// A CORS-blocked response reaches here as a TypeError with no status,
		// the same shape as a dropped connection.
		const fetchImpl = vi.fn(async () => {
			throw new TypeError("Failed to fetch");
		});

		expect(await probeMediaSource(src, fetchImpl)).toBe("failed");
	});
});

describe("isPlayableVideo", () => {
	it("plays finished recordings that are still on disk", () => {
		expect(isPlayableVideo(makeVideo(0))).toBe(true);
		expect(isPlayableVideo(makeVideo(0, VIDEO_STATES.partial))).toBe(true);
	});

	it("refuses recordings that are unfinished, failed, removed or missing", () => {
		for (const state of [
			VIDEO_STATES.recording,
			VIDEO_STATES.queued,
			VIDEO_STATES.failed,
			VIDEO_STATES.cancelled,
			VIDEO_REMOVAL_STATES.removed,
			VIDEO_REMOVAL_STATES.missing,
		]) {
			expect(isPlayableVideo(makeVideo(0, state))).toBe(false);
		}
		expect(isPlayableVideo(undefined)).toBe(false);
		expect(isPlayableVideo(null)).toBe(false);
	});
});
