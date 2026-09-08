import { describe, expect, it } from "vitest";
import {
	buildDirectDownloadPayload,
	DirectDownloadFormSchema,
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
