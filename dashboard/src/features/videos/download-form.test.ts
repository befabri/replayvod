import { describe, expect, it } from "vitest";
import { buildDirectDownloadPayload } from "./download-form";

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
