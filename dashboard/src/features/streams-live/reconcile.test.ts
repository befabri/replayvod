import { describe, expect, it } from "vitest";
import { type LiveDelta, reconcileLiveSet } from "./queries";

const delta = (online: boolean, at: number): LiveDelta => ({ online, at });

describe("reconcileLiveSet", () => {
	it("uses the snapshot as the base when there are no deltas", () => {
		const result = reconcileLiveSet(["a", "b"], 10, new Map());
		expect([...result].sort()).toEqual(["a", "b"]);
	});

	it("drops a stale delta that the newer snapshot supersedes (the bug)", () => {
		// X was marked online at t=5, then went offline and the snapshot at t=10
		// omits it. The stale online delta must NOT be replayed on top.
		const deltas = new Map([["x", delta(true, 5)]]);
		const result = reconcileLiveSet([], 10, deltas);
		expect(result.has("x")).toBe(false);
		// And the superseded delta is pruned so it can't resurface later.
		expect(deltas.has("x")).toBe(false);
	});

	it("keeps an online delta that raced ahead of the snapshot", () => {
		// Delta arrived at t=15, after the snapshot was received at t=10.
		const deltas = new Map([["x", delta(true, 15)]]);
		const result = reconcileLiveSet([], 10, deltas);
		expect(result.has("x")).toBe(true);
		expect(deltas.has("x")).toBe(true);
	});

	it("applies an offline delta that raced ahead of the snapshot", () => {
		// Snapshot still lists "x", but a newer offline delta removes it.
		const deltas = new Map([["x", delta(false, 15)]]);
		const result = reconcileLiveSet(["x"], 10, deltas);
		expect(result.has("x")).toBe(false);
	});

	it("treats a delta at the exact snapshot time as superseded", () => {
		const deltas = new Map([["x", delta(true, 10)]]);
		const result = reconcileLiveSet([], 10, deltas);
		expect(result.has("x")).toBe(false);
		expect(deltas.has("x")).toBe(false);
	});

	it("prunes superseded deltas while keeping the ones that raced ahead", () => {
		const deltas = new Map([
			["old", delta(true, 5)], // superseded -> pruned, not applied
			["fresh", delta(true, 20)], // raced ahead -> kept + applied
		]);
		const result = reconcileLiveSet(["base"], 10, deltas);
		expect([...result].sort()).toEqual(["base", "fresh"]);
		expect(deltas.has("old")).toBe(false);
		expect(deltas.has("fresh")).toBe(true);
	});
});
