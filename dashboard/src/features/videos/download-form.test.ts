import { describe, expect, it } from "vitest";
import {
	buildDirectDownloadPayload,
	DirectDownloadFormSchema,
	resolveDirectDownloadQuality,
} from "./download-form";

describe("buildDirectDownloadPayload", () => {
	it("forwards quality and force_h264 for a video download", () => {
		expect(
			buildDirectDownloadPayload("b-1", {
				recording_type: "video",
				quality: "HIGH",
				force_h264: true,
			}),
		).toEqual({
			broadcaster_id: "b-1",
			recording_type: "video",
			quality: "HIGH",
			force_h264: true,
		});
	});

	it("clears stale force_h264 for an audio download", () => {
		expect(
			buildDirectDownloadPayload("b-1", {
				recording_type: "audio",
				quality: "HIGH",
				force_h264: true,
			}),
		).toEqual({
			broadcaster_id: "b-1",
			recording_type: "audio",
			quality: "HIGH",
			force_h264: false,
		});
	});

	it("sends a pinned height with the tier it falls under", () => {
		expect(
			buildDirectDownloadPayload("b-1", {
				recording_type: "video",
				quality: 936,
				force_h264: false,
			}),
		).toEqual({
			broadcaster_id: "b-1",
			recording_type: "video",
			quality: "HIGH",
			force_h264: false,
			max_height: 936,
		});
	});

	it("drops a pinned height for an audio download", () => {
		expect(
			buildDirectDownloadPayload("b-1", {
				recording_type: "audio",
				quality: 1440,
				force_h264: false,
			}),
		).toEqual({
			broadcaster_id: "b-1",
			recording_type: "audio",
			quality: "1440",
			force_h264: false,
		});
	});
});

it.each([
	"1440",
	"BEST",
])("validates and submits %s without narrowing it to HIGH", (quality) => {
	const values = DirectDownloadFormSchema.parse({
		recording_type: "video",
		quality,
		force_h264: false,
	});
	expect(buildDirectDownloadPayload("123", values).quality).toBe(quality);
});
it.each(["", "ULTRA", "2160"])("rejects unsupported quality %s", (quality) => {
	expect(
		DirectDownloadFormSchema.safeParse({
			recording_type: "video",
			quality,
			force_h264: false,
		}).success,
	).toBe(false);
});
it.each([0, -1, 1.5, 4321])("rejects a pinned height of %s", (quality) => {
	expect(
		DirectDownloadFormSchema.safeParse({
			recording_type: "video",
			quality,
			force_h264: false,
		}).success,
	).toBe(false);
});

it.each([
	480, 720, 1080, 1440,
])("preserves an exact %ip ceiling when rendition lookup fails", (height) => {
	const values = {
		recording_type: "video" as const,
		quality: height,
		force_h264: true,
	};
	const quality = resolveDirectDownloadQuality(values, true, {
		data: undefined,
		status: "error",
		fetchStatus: "idle",
	});
	expect(quality.kind).toBe("ceiling");
	if (quality.kind !== "ceiling") throw new Error("expected a ceiling");
	if (quality.quality === null) throw new Error("expected the exact ceiling");
	expect(
		buildDirectDownloadPayload("b1", { ...values, quality: quality.quality }),
	).not.toHaveProperty("max_height");
});
it.each([
	360, 936, 1441, 2160, 4320,
])("does not round a %ip pin up when rendition lookup fails", (height) => {
	expect(
		resolveDirectDownloadQuality(
			{ recording_type: "video", quality: height, force_h264: true },
			true,
			{ data: undefined, status: "error", fetchStatus: "idle" },
		),
	).toEqual({ kind: "ceiling", quality: null });
});
