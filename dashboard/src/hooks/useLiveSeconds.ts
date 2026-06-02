import { useRef } from "react";
import { useTick } from "@/hooks/useTick";

// useLiveSeconds extrapolates a seconds value forward between server samples so a
// live counter keeps moving instead of freezing until the next push. It
// re-renders about once a second (via the shared useTick timer) and returns the
// sampled value plus the wall-clock seconds elapsed since that sample, re-syncing
// whenever a fresh sample updates `base`/`sampleAtMs`.
//
// This only holds for quantities that advance ~1:1 with wall time — i.e. a live
// recording's media clock. Pass active=false to hold the value static (no media
// clock to extrapolate, or before the first sample lands).
export function useLiveSeconds(
	base: number,
	sampleAtMs: number,
	active = true,
): number {
	// Always subscribe so hook order stays stable; the tick is harmless when the
	// value is held static.
	useTick(1000);

	// Monotonic floor: a live elapsed counter must never tick backward. A new SSE
	// push advances sampleAtMs; if it carries the same base (the server re-emits an
	// unchanged media offset, or the media clock stalled during a gap) the raw
	// extrapolation snaps back down to base. Holding the previous maximum keeps the
	// counter — and the band widths it drives — from jumping backward. Reset while
	// held static so a later activation (or a remounted row for a new recording)
	// starts fresh.
	const maxRef = useRef(0);
	if (!active || !Number.isFinite(sampleAtMs) || sampleAtMs <= 0) {
		maxRef.current = base;
		return base;
	}
	const extrapolated = base + Math.max(0, (Date.now() - sampleAtMs) / 1000);
	const value = Math.max(maxRef.current, extrapolated);
	maxRef.current = value;
	return value;
}
