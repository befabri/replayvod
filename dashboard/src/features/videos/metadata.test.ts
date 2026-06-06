import { describe, expect, it } from "vitest";
import type { VideoResponse } from "@/api/generated/trpc";
import {
	isMultipartVideo,
	recordingDurationSeconds,
	recordingQualitySummary,
	recordingSizeBytes,
	videoPartCount,
} from "./metadata";

describe("recording metadata helpers", () => {
	it("reports multipart count and aggregate row values", () => {
		const v = video({
			duration_seconds: undefined,
			size_bytes: undefined,
			parts: [
				part({ part_index: 2, duration_seconds: 30, size_bytes: 300 }),
				part({ part_index: 1, duration_seconds: 10, size_bytes: 100 }),
			],
		});

		expect(isMultipartVideo(v)).toBe(true);
		expect(videoPartCount(v)).toBe(2);
		expect(recordingDurationSeconds(v)).toBe(40);
		expect(recordingSizeBytes(v)).toBe(400);
	});

	it("prefers stored video totals over summed parts", () => {
		const v = video({
			duration_seconds: 99,
			size_bytes: 999,
			parts: [part({ part_index: 1, duration_seconds: 10, size_bytes: 100 })],
		});

		expect(recordingDurationSeconds(v)).toBe(99);
		expect(recordingSizeBytes(v)).toBe(999);
	});

	it("summarizes mixed part quality", () => {
		const v = video({
			parts: [
				part({ part_index: 1, quality: "1080p60" }),
				part({ part_index: 2, quality: "720p60" }),
				part({ part_index: 3, quality: "1080p60" }),
			],
		});

		expect(recordingQualitySummary(v, "Mixed")).toBe("Mixed (1080p60, 720p60)");
	});
});

function video(partial: Partial<VideoResponse>): VideoResponse {
	return {
		id: 1,
		job_id: "job",
		filename: "vod",
		display_name: "Streamer",
		title: "Title",
		status: "DONE",
		completion_kind: "complete",
		truncated: false,
		quality: "1080p60",
		is_audio_only: false,
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
