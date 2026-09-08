import { describe, expect, it } from "vitest";
import { chunk, MAX_VODS_PER_ENQUEUE } from "./limits";

describe("chunk", () => {
	it("splits into consecutive groups of at most size", () => {
		expect(chunk([1, 2, 3, 4, 5], 2)).toEqual([[1, 2], [3, 4], [5]]);
		expect(chunk([1, 2], 2)).toEqual([[1, 2]]);
		expect(chunk([], 2)).toEqual([]);
	});

	it("matches the server's per-call ceiling", () => {
		const ids = Array.from({ length: 101 }, (_, i) => String(i));
		const groups = chunk(ids, MAX_VODS_PER_ENQUEUE);
		expect(groups.map((g) => g.length)).toEqual([50, 50, 1]);
	});
});
