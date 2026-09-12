import { describe, expect, it } from "vitest";
import {
	HISTORY_MEDIA_SCOPES,
	HISTORY_OUTCOMES,
	historyEmptyKey,
	historyFilters,
	historyTabCounts,
	validateHistorySearch,
} from "./history";

describe("history controls", () => {
	it("validates outcome and media independently", () => {
		expect(
			validateHistorySearch({ outcome: "failed", media: "garbage" }),
		).toEqual({ outcome: "failed", media: "any" });
		expect(validateHistorySearch({ outcome: [], media: "removed" })).toEqual({
			outcome: "all",
			media: "removed",
		});
		expect(validateHistorySearch({})).toEqual({ outcome: "all", media: "any" });
		for (const outcome of HISTORY_OUTCOMES)
			for (const media of HISTORY_MEDIA_SCOPES) {
				expect(validateHistorySearch({ outcome, media })).toEqual({
					outcome,
					media,
				});
				expect(historyFilters({ outcome, media })).toEqual({
					outcome: outcome === "all" ? undefined : outcome,
					scope: {
						any: "all",
						on_disk: "active",
						removed: "removed",
						unavailable: "removed",
					}[media],
					...(media === "unavailable" ? { deletionKind: "missing" } : {}),
				});
			}
	});

	it("explains the selected empty view, preferring removed media", () => {
		for (const outcome of HISTORY_OUTCOMES)
			expect(historyEmptyKey({ outcome, media: "removed" })).toBe(
				"history.empty_removed",
			);
		expect(historyEmptyKey({ outcome: "all", media: "on_disk" })).toBe(
			"history.empty_on_disk",
		);
		expect(historyEmptyKey({ outcome: "failed", media: "on_disk" })).toBe(
			"history.empty_failed",
		);
		expect(historyEmptyKey({ outcome: "cancelled", media: "any" })).toBe(
			"history.empty_cancelled",
		);
	});

	it("scopes tab counts without pretending an unloaded count is zero", () => {
		const counts = {
			all: { on_disk: 8, removed: 4, unavailable: 2 },
			completed: { on_disk: 5, removed: 1, unavailable: 0 },
			failed: { on_disk: 2, removed: 3, unavailable: 2 },
			cancelled: { on_disk: 1, removed: 0, unavailable: 0 },
		};
		expect(historyTabCounts(undefined, "any")).toEqual({
			all: undefined,
			failed: undefined,
			cancelled: undefined,
		});
		expect(historyTabCounts(counts, "any")).toEqual({
			all: 12,
			failed: 5,
			cancelled: 1,
		});
		expect(historyTabCounts(counts, "on_disk")).toEqual({
			all: 8,
			failed: 2,
			cancelled: 1,
		});
		expect(historyTabCounts(counts, "removed")).toEqual({
			all: 4,
			failed: 3,
			cancelled: 0,
		});
	});
});

describe("unavailable media scope", () => {
	it("is the removed scope narrowed to missing media", () => {
		expect(historyFilters({ outcome: "all", media: "unavailable" })).toEqual({
			outcome: undefined,
			scope: "removed",
			deletionKind: "missing",
		});
		expect(historyEmptyKey({ outcome: "failed", media: "unavailable" })).toBe(
			"history.empty_unavailable",
		);
	});

	it("labels the tabs with the restorable slice of the removed counts", () => {
		const counts = {
			all: { on_disk: 8, removed: 4, unavailable: 3 },
			completed: { on_disk: 5, removed: 1, unavailable: 1 },
			failed: { on_disk: 2, removed: 3, unavailable: 2 },
			cancelled: { on_disk: 1, removed: 0, unavailable: 0 },
		};
		expect(historyTabCounts(counts, "unavailable")).toEqual({
			all: 3,
			failed: 2,
			cancelled: 0,
		});
		expect(historyTabCounts(counts, "any")).toEqual({
			all: 12,
			failed: 5,
			cancelled: 1,
		});
	});
});
