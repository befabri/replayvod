import { describe, expect, it } from "vitest";
import type { LiveRendition } from "@/api/generated/trpc";
import {
	renditionAtOrBelow,
	renditionLabel,
	renditionOptions,
} from "./renditions";

// Server order: tallest first, each height's preferred variant ahead of its
// alternatives.
const stream: LiveRendition[] = [
	{ height: 1440, fps: 60, codec: "h265" },
	{ height: 1080, fps: 60, codec: "h265" },
	{ height: 1080, fps: 60, codec: "h264" },
	{ height: 720, fps: 60, codec: "h264" },
	{ height: 720, fps: 30, codec: "h264" },
	{ height: 480, codec: "h264" },
];

describe("renditionOptions", () => {
	it("keeps one row per height, the preferred variant, tallest first", () => {
		expect(renditionOptions(stream, false)).toEqual([
			{ height: 1440, fps: 60, codec: "h265" },
			{ height: 1080, fps: 60, codec: "h265" },
			{ height: 720, fps: 60, codec: "h264" },
			{ height: 480, fps: undefined, codec: "h264" },
		]);
	});

	it("drops non-H.264 variants under Force H.264, losing HEVC-only heights", () => {
		expect(renditionOptions(stream, true).map((o) => o.height)).toEqual([
			1080, 720, 480,
		]);
		expect(renditionOptions(stream, true)[0]?.codec).toBe("h264");
	});

	it("sorts a list the server left unordered", () => {
		expect(
			renditionOptions(
				[
					{ height: 480, codec: "h264" },
					{ height: 1080, codec: "h264" },
				],
				false,
			).map((o) => o.height),
		).toEqual([1080, 480]);
	});
});

describe("renditionAtOrBelow", () => {
	it("picks the tallest height at or under 1080p", () => {
		expect(renditionAtOrBelow(renditionOptions(stream, false), 1080)).toBe(
			1080,
		);
	});

	it("requires a choice when no available rendition fits the ceiling", () => {
		expect(
			renditionAtOrBelow([{ height: 1440, codec: "h265" }], 1080),
		).toBeNull();
		expect(renditionAtOrBelow([], 1080)).toBeNull();
	});

	it.each([480, 720, 1440])("honours a %ip ceiling", (height) => {
		expect(renditionAtOrBelow(renditionOptions(stream, false), height)).toBe(
			height,
		);
	});

	it("honours a nonstandard height and an unlimited ceiling", () => {
		expect(renditionAtOrBelow(renditionOptions(stream, false), 936)).toBe(720);
		expect(renditionAtOrBelow(renditionOptions(stream, false), Infinity)).toBe(
			1440,
		);
	});
});

describe("renditionLabel", () => {
	it("reads like Twitch's quality menu", () => {
		expect(renditionLabel({ height: 1080, fps: 60, codec: "h264" })).toBe(
			"1080p60",
		);
		expect(renditionLabel({ height: 720, fps: 30, codec: "h264" })).toBe(
			"720p",
		);
		expect(renditionLabel({ height: 480, codec: "h264" })).toBe("480p");
		expect(renditionLabel({ height: 1440, fps: 59.94, codec: "h265" })).toBe(
			"1440p60 · HEVC",
		);
		expect(renditionLabel({ height: 1080, fps: 60, codec: "av1" })).toBe(
			"1080p60 · AV1",
		);
	});
});
