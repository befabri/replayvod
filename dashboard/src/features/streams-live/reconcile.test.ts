import { describe, expect, it } from "vitest";
import { LiveStatusReconciler } from "./queries";

describe("live snapshot ordering", () => {
	it("preserves online and offline events received during a snapshot request", () => {
		const state = new LiveStatusReconciler();
		const revision = state.beginSnapshot();
		state.record("new", true);
		state.record("old", false);
		expect(state.reconcile(["old", "unchanged"], revision)).toEqual([
			"unchanged",
			"new",
		]);
	});
	it("lets a later snapshot supersede events from before its request", () => {
		const state = new LiveStatusReconciler();
		state.record("stale", true);
		const revision = state.beginSnapshot();
		expect(state.reconcile(["fresh"], revision)).toEqual(["fresh"]);
		expect(state.reconcile([], state.beginSnapshot())).toEqual([]);
	});
	it("keeps only the latest transition per broadcaster", () => {
		const state = new LiveStatusReconciler();
		const revision = state.beginSnapshot();
		state.record("a", true);
		state.record("a", false);
		state.record("b", false);
		state.record("b", true);
		expect(state.reconcile(["a"], revision)).toEqual(["b"]);
	});
});
