// @vitest-environment jsdom

import { act, cleanup, renderHook } from "@testing-library/react";
import { afterEach, describe, expect, it } from "vitest";
import {
	archivePayloadSettings,
	DEFAULT_ARCHIVE_SETTINGS,
	useArchiveSettings,
} from "./settings";

afterEach(cleanup);

describe("useArchiveSettings", () => {
	it("starts from the defaults and stores quality as given", () => {
		const { result } = renderHook(() => useArchiveSettings());
		expect(result.current.settings).toEqual(DEFAULT_ARCHIVE_SETTINGS);
		act(() => result.current.setQuality("BEST"));
		expect(result.current.settings).toEqual({
			...DEFAULT_ARCHIVE_SETTINGS,
			quality: "BEST",
		});
	});

	it("keeps force_h264 for video and clears it for audio", () => {
		const { result } = renderHook(() => useArchiveSettings());
		act(() => result.current.setForceH264(true));
		expect(result.current.settings.force_h264).toBe(true);

		act(() => result.current.setRecordingType("audio"));
		expect(result.current.settings.recording_type).toBe("audio");
		expect(result.current.settings.force_h264).toBe(false);

		// Audio has no video track to transcode, so the flag cannot be set.
		act(() => result.current.setForceH264(true));
		expect(result.current.settings.force_h264).toBe(false);

		// Switching back does not resurrect the choice audio dropped.
		act(() => result.current.setRecordingType("video"));
		expect(result.current.settings.force_h264).toBe(false);
		act(() => result.current.setForceH264(true));
		expect(result.current.settings.force_h264).toBe(true);
	});

	it("accepts an initial state and applies the same coupling to it", () => {
		const { result } = renderHook(() =>
			useArchiveSettings({
				recording_type: "audio",
				quality: "LOW",
				force_h264: true,
			}),
		);
		// The initial value is taken as given; only writes are normalised.
		expect(result.current.settings.force_h264).toBe(true);
		act(() => result.current.setQuality("MEDIUM"));
		expect(result.current.settings).toEqual({
			recording_type: "audio",
			quality: "MEDIUM",
			force_h264: true,
		});
		act(() => result.current.setForceH264(true));
		expect(result.current.settings.force_h264).toBe(false);
	});
});

describe("archivePayloadSettings", () => {
	it("sends force_h264 only for video", () => {
		expect(
			archivePayloadSettings({
				recording_type: "video",
				quality: "HIGH",
				force_h264: true,
			}),
		).toEqual({ recording_type: "video", quality: "HIGH", force_h264: true });
		expect(
			archivePayloadSettings({
				recording_type: "audio",
				quality: "HIGH",
				force_h264: true,
			}),
		).toEqual({ recording_type: "audio", quality: "HIGH", force_h264: false });
	});
});
