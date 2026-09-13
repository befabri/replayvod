import type { TimelineEvent } from "@/api/generated/trpc";

export type TimelineChangedField = "category" | "title";
export type TimelineMarkerKind = "category" | "title" | "mixed";

export type TimelineChangeEvent = {
	event: TimelineEvent;
	changes: TimelineChangedField[];
};

export function timelineEventOffsetSeconds(
	event: Pick<TimelineEvent, "occurred_at" | "media_offset_seconds">,
	wallClockAnchor: string | number,
): number {
	if (
		typeof event.media_offset_seconds === "number" &&
		Number.isFinite(event.media_offset_seconds)
	) {
		return Math.max(0, Math.round(event.media_offset_seconds));
	}
	const anchorMs =
		typeof wallClockAnchor === "number"
			? wallClockAnchor
			: new Date(wallClockAnchor).getTime();
	const occurredMs = new Date(event.occurred_at).getTime();
	// Unparseable timestamps yield NaN, which slips past downstream `< 0` and
	// clamp() guards and renders a marker at left: NaN%. Return a negative
	// sentinel instead so those guards reject (or clamp) it like any bad offset.
	if (!Number.isFinite(anchorMs) || !Number.isFinite(occurredMs)) return -1;
	return Math.max(0, Math.round((occurredMs - anchorMs) / 1000));
}

export function timelineEventKey(event: TimelineEvent): string {
	return [
		event.occurred_at,
		event.media_offset_seconds ?? "wall",
		event.category?.id ?? "no-category",
		event.title?.id ?? event.title?.name ?? "no-title",
	].join(":");
}

export function timelineChangeEvents(
	events: TimelineEvent[] | undefined,
): TimelineChangeEvent[] {
	const rows = dedupConsecutive(
		sortTimelineEvents(events ?? []),
		eventStateKey,
	);
	const out: TimelineChangeEvent[] = [];
	let lastCategoryId: string | null = null;
	let lastTitleName: string | null = null;

	for (const event of rows) {
		const changes: TimelineChangedField[] = [];
		const categoryId = event.category?.id ?? null;
		const titleName = cleanName(event.title?.name);

		if (event.category && categoryId !== lastCategoryId) {
			changes.push("category");
		}
		if (event.title && titleName !== lastTitleName) {
			changes.push("title");
		}

		if (event.category) lastCategoryId = categoryId;
		if (event.title) lastTitleName = titleName;
		if (changes.length > 0) out.push({ event, changes });
	}

	return out;
}

export function timelineMarkerLabel(
	event: TimelineEvent,
	changes: TimelineChangedField[],
): string {
	const category = event.category?.name.trim();
	const title = event.title?.name.trim();
	const labels: string[] = [];
	if (changes.includes("category") && category) labels.push(category);
	if (changes.includes("title") && title) labels.push(title);
	return labels.join(" - ");
}

export function timelineMarkerKind(
	changes: TimelineChangedField[],
): TimelineMarkerKind {
	if (changes.includes("category") && changes.includes("title")) return "mixed";
	if (changes.includes("category")) return "category";
	return "title";
}

export function dedupConsecutive<T>(
	items: T[],
	keyOf: (item: T) => unknown,
): T[] {
	const out: T[] = [];
	for (const item of items) {
		const last = out[out.length - 1];
		if (last && keyOf(last) === keyOf(item)) continue;
		out.push(item);
	}
	return out;
}

export function eventStateKey(event: TimelineEvent): string {
	return `${event.category?.id ?? ""}:${cleanName(event.title?.name) ?? ""}`;
}

function cleanName(name: string | undefined): string | null {
	const trimmed = name?.trim();
	return trimmed ? trimmed : null;
}

function sortTimelineEvents(events: TimelineEvent[]): TimelineEvent[] {
	return [...events].sort((a, b) => {
		const byTime =
			new Date(a.occurred_at).getTime() - new Date(b.occurred_at).getTime();
		if (byTime !== 0) return byTime;
		return (a.media_offset_seconds ?? 0) - (b.media_offset_seconds ?? 0);
	});
}
