// @vitest-environment jsdom

import { renderHook } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { useLiveSeconds } from "./useLiveSeconds";

describe("useLiveSeconds", () => {
	beforeEach(() => {
		vi.useFakeTimers();
		vi.setSystemTime(0);
	});
	afterEach(() => {
		vi.useRealTimers();
	});

	it("extrapolates forward from the last sample", () => {
		vi.setSystemTime(1000);
		const { result, rerender } = renderHook(
			({ base, sampleAt }) => useLiveSeconds(base, sampleAt, true),
			{ initialProps: { base: 100, sampleAt: 1000 } },
		);
		expect(result.current).toBe(100);

		vi.setSystemTime(6000); // 5s later
		rerender({ base: 100, sampleAt: 1000 });
		expect(result.current).toBe(105);
	});

	// Regression: a fresh push that repeats the same base (unchanged media offset
	// / clock stalled in a gap) must not snap the counter backward to base.
	it("never ticks backward when a new sample repeats the same base", () => {
		vi.setSystemTime(1000);
		const { result, rerender } = renderHook(
			({ base, sampleAt }) => useLiveSeconds(base, sampleAt, true),
			{ initialProps: { base: 100, sampleAt: 1000 } },
		);

		vi.setSystemTime(6000);
		rerender({ base: 100, sampleAt: 1000 });
		expect(result.current).toBe(105);

		// New SSE push at t=6000 carrying the same base. Raw extrapolation would be
		// 100 + 0 = 100; the monotonic floor must hold ≥ 105.
		rerender({ base: 100, sampleAt: 6000 });
		expect(result.current).toBeGreaterThanOrEqual(105);
	});

	it("follows a base that advances past the held value", () => {
		vi.setSystemTime(1000);
		const { result, rerender } = renderHook(
			({ base, sampleAt }) => useLiveSeconds(base, sampleAt, true),
			{ initialProps: { base: 100, sampleAt: 1000 } },
		);
		vi.setSystemTime(6000);
		rerender({ base: 100, sampleAt: 1000 }); // ~105
		rerender({ base: 200, sampleAt: 6000 }); // base jumped well past 105
		expect(result.current).toBe(200);
	});
});
