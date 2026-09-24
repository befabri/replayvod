import { describe, expect, it } from "vitest";
import { makeVideo, VIDEO_QUALITIES } from "./videos";

describe("makeVideo", () => {
	it("marks exactly the audio_only quality as audio only", () => {
		for (let index = 0; index < VIDEO_QUALITIES.length; index++) {
			const video = makeVideo(index);
			expect(video.is_audio_only).toBe(video.quality === "audio_only");
		}
	});

	// A quality override must not keep the audio flag the index would have
	// picked, or a "1080p" row ends up audio only.
	it("keeps the audio flag in step with an overridden quality", () => {
		for (let index = 0; index < VIDEO_QUALITIES.length; index++) {
			expect(makeVideo(index, { quality: "1080p" }).is_audio_only).toBe(false);
			expect(makeVideo(index, { quality: "audio_only" }).is_audio_only).toBe(
				true,
			);
		}
	});

	it("still lets a caller set the audio flag on its own", () => {
		expect(
			makeVideo(0, { quality: "1080p", is_audio_only: true }).is_audio_only,
		).toBe(true);
	});
});
