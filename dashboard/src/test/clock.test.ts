import { afterEach, describe, expect, it } from "vitest";
import { startClockAt } from "./clock";

const START = Date.UTC(2001, 0, 1);

describe("startClockAt", () => {
	let restore: (() => void) | undefined;

	afterEach(() => {
		restore?.();
		restore = undefined;
	});

	it("starts Date.now and new Date() at the given instant", () => {
		restore = startClockAt(START);
		expect(Date.now() - START).toBeGreaterThanOrEqual(0);
		expect(Date.now() - START).toBeLessThan(1000);
		expect(new Date().toISOString()).toMatch(/^2001-01-01T00:00:0/);
	});

	it("keeps the clock moving so timers and durations stay real", async () => {
		restore = startClockAt(START);
		const before = Date.now();
		await new Promise((resolve) => setTimeout(resolve, 20));
		expect(Date.now() - before).toBeGreaterThanOrEqual(15);
	});

	it("leaves explicit dates, statics and instanceof untouched", () => {
		restore = startClockAt(START);
		const iso = "2024-03-01T00:00:00.000Z";
		expect(new Date(iso).toISOString()).toBe(iso);
		expect(new Date(2024, 2, 1).getFullYear()).toBe(2024);
		expect(Date.UTC(2024, 2, 1)).toBe(Date.parse(iso));
		expect(new Date()).toBeInstanceOf(Date);
		expect(typeof Date()).toBe("string");
	});

	it("restores the real clock", () => {
		const RealDate = Date;
		startClockAt(START)();
		expect(Date).toBe(RealDate);
		expect(Math.abs(Date.now() - START)).toBeGreaterThan(1000);
	});
});
