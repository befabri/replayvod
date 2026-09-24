import { describe, expect, it } from "vitest";
import { recordingPosterURL } from "./thumbnail";

describe("recordingPosterURL", () => {
	it("serves the thumbnail the recording points at", () => {
		expect(recordingPosterURL({ thumbnail: "thumbnails/rec-snap03.jpg" })).toBe(
			"/api/v1/thumbnails/rec-snap03.jpg",
		);
	});

	// The server records the first snapshot it actually wrote as the
	// thumbnail, so an empty field means no image exists. Guessing a snapshot
	// name would request a 404 and make the player reserve room for nothing.
	it("gives no poster when the recording has no thumbnail", () => {
		expect(recordingPosterURL({ thumbnail: undefined })).toBeNull();
		expect(recordingPosterURL({ thumbnail: "" })).toBeNull();
	});
});
