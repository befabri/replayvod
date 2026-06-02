import { type ClassValue, clsx } from "clsx";
import { twMerge } from "tailwind-merge";

export function cn(...inputs: ClassValue[]) {
	return twMerge(clsx(inputs));
}

export type PopoverAlign = "start" | "center" | "end";

export function clamp(value: number, min: number, max: number): number {
	if (max < min) return min;
	return Math.min(max, Math.max(min, value));
}

export function isFinitePositive(
	value: number | undefined | null,
): value is number {
	return typeof value === "number" && Number.isFinite(value) && value > 0;
}

export function positiveOrZero(value: number | undefined | null): number {
	return isFinitePositive(value) ? value : 0;
}

export function positiveOrNull(
	value: number | undefined | null,
): number | null {
	return isFinitePositive(value) ? value : null;
}

export function clampPercent(value: number): number {
	return clamp(value, 0, 100);
}

export function percentOf(value: number, total: number): number {
	if (total <= 0) return 0;
	return clampPercent((value / total) * 100);
}

export function popoverAlign(leftPercent: number): PopoverAlign {
	if (leftPercent < 14) return "start";
	if (leftPercent > 86) return "end";
	return "center";
}
