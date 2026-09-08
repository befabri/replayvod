import { describe, expect, it } from "vitest";
import { parseChannelInput, parseVodId, parseVodLines } from "./parse";

describe("parseVodId", () => {
	it.each([
		["123456789", "123456789"],
		["  123456789 ", "123456789"],
		["https://www.twitch.tv/videos/2233445566", "2233445566"],
		["http://twitch.tv/videos/2233445566/", "2233445566"],
		["twitch.tv/videos/2233445566", "2233445566"],
		["https://m.twitch.tv/videos/2233445566?t=1h2m3s", "2233445566"],
		["https://www.twitch.tv/videos/2233445566#x", "2233445566"],
		["https://www.twitch.tv/somestreamer/video/2233445566", "2233445566"],
		["https://www.twitch.tv/somestreamer/v/2233445566", "2233445566"],
	])("accepts %s", (input, want) => {
		expect(parseVodId(input)).toBe(want);
	});

	it.each([
		"",
		"abc",
		"12a34",
		"https://www.twitch.tv/somestreamer",
		"https://www.twitch.tv/somestreamer/clip/Funny",
		"https://clips.twitch.tv/Funny",
		"https://www.youtube.com/watch?v=123",
		"https://evil-twitch.tv/videos/123",
		"https://www.twitch.tv/videos/",
		"https://www.twitch.tv/videos/123/extra",
	])("rejects %s", (input) => {
		expect(parseVodId(input)).toBeNull();
	});
});

describe("parseChannelInput", () => {
	it.each([
		["somestreamer", "somestreamer"],
		["SomeStreamer", "somestreamer"],
		["@somestreamer", "somestreamer"],
		[
			"https://www.twitch.tv/SomeStreamer/videos?filter=archives",
			"somestreamer",
		],
		["twitch.tv/somestreamer/", "somestreamer"],
	])("accepts %s", (input, want) => {
		expect(parseChannelInput(input)).toBe(want);
	});

	it.each([
		"",
		"@",
		"a",
		"has space",
		"https://www.twitch.tv/videos/123",
		"https://www.twitch.tv/somestreamer/video/123",
		"https://www.twitch.tv/directory/category/x",
		"https://www.twitch.tv/",
		"https://example.com/somestreamer",
	])("rejects %s", (input) => {
		expect(parseChannelInput(input)).toBeNull();
	});
});

describe("parseVodLines", () => {
	it("splits on newlines and commas and keeps invalid lines for feedback", () => {
		const lines = parseVodLines(
			"https://www.twitch.tv/videos/1\n\n2, not-a-link\n  \n",
		);
		expect(lines).toEqual([
			{ input: "https://www.twitch.tv/videos/1", vodId: "1" },
			{ input: "2", vodId: "2" },
			{ input: "not-a-link", vodId: null },
		]);
	});

	it("splits links pasted on one line separated by spaces", () => {
		const lines = parseVodLines(
			"https://www.twitch.tv/videos/1 https://www.twitch.tv/videos/2",
		);
		expect(lines.map((l) => l.vodId)).toEqual(["1", "2"]);
	});

	it("returns nothing for blank text", () => {
		expect(parseVodLines("  \n ")).toEqual([]);
	});
});
