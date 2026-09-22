import { useRef } from "react";
import { useTick } from "@/hooks/useTick";

export function useLiveSeconds(
	base: number,
	sampleAtMs: number,
	active = true,
): number {
	useTick(1000);

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
