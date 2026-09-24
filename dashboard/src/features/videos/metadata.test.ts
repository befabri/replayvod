import { describe, expect, it } from "vitest";
import type { VideoResponse } from "@/api/generated/trpc";
import { makeVideo, makeVideoPart } from "@/test/fixtures";
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
				makeVideoPart({ part_index: 2, duration_seconds: 30, size_bytes: 300 }),
				makeVideoPart({ part_index: 1, duration_seconds: 10, size_bytes: 100 }),
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
			parts: [
				makeVideoPart({ part_index: 1, duration_seconds: 10, size_bytes: 100 }),
			],
		});

		expect(recordingDurationSeconds(v)).toBe(99);
		expect(recordingSizeBytes(v)).toBe(999);
	});

	it("summarizes mixed part quality", () => {
		const v = video({
			parts: [
				makeVideoPart({ part_index: 1, quality: "1080p60" }),
				makeVideoPart({ part_index: 2, quality: "720p60" }),
				makeVideoPart({ part_index: 3, quality: "1080p60" }),
			],
		});

		expect(recordingQualitySummary(v, "Mixed")).toBe("Mixed (1080p60, 720p60)");
	});
});

function video(partial: Partial<VideoResponse>): VideoResponse {
	return makeVideo(0, {
		quality: "1080p60",
		duration_seconds: undefined,
		size_bytes: undefined,
		...partial,
	});
}
