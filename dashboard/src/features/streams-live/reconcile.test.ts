import { describe, expect, it } from "vitest";
import { LiveStatusReconciler } from "./queries";

describe("live snapshot ordering", () => {
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
