import { describe, expect, it } from "vitest";
import { resolveBoxArtSrcSet, resolveBoxArtUrl } from "./twitch";

describe("resolveBoxArtUrl", () => {
	it("fills Twitch box-art templates with the requested dimensions", () => {
		expect(
			resolveBoxArtUrl(
				"https://static-cdn.jtvnw.net/ttv-boxart/509658-{width}x{height}.jpg",
				144,
				192,
			),
		).toBe("https://static-cdn.jtvnw.net/ttv-boxart/509658-144x192.jpg");
	});

	it("passes through pre-sized URLs", () => {
		expect(resolveBoxArtUrl("https://example.test/art.jpg", 144, 192)).toBe(
			"https://example.test/art.jpg",
		);
	});
});

describe("resolveBoxArtSrcSet", () => {
	it("builds responsive width variants for category art", () => {
		expect(
			resolveBoxArtSrcSet(
				"https://static-cdn.jtvnw.net/ttv-boxart/509658-{width}x{height}.jpg",
				144,
				192,
			),
		).toBe(
			[
				"https://static-cdn.jtvnw.net/ttv-boxart/509658-144x192.jpg 144w",
				"https://static-cdn.jtvnw.net/ttv-boxart/509658-216x288.jpg 216w",
				"https://static-cdn.jtvnw.net/ttv-boxart/509658-288x384.jpg 288w",
				"https://static-cdn.jtvnw.net/ttv-boxart/509658-432x576.jpg 432w",
			].join(", "),
		);
	});

	it("omits srcset when the URL is missing", () => {
		expect(resolveBoxArtSrcSet(null, 144, 192)).toBeUndefined();
	});

	it("omits srcset for pre-sized URLs", () => {
		expect(
			resolveBoxArtSrcSet("https://example.test/art.jpg", 144, 192),
		).toBeUndefined();
	});
});
