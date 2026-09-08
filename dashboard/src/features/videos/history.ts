import type { HistoryCountsResponse } from "@/api/generated/trpc";
import type {
	HistoryMedia,
	HistoryOutcome,
	HistoryView,
} from "./components/activityColumns";
import type { VideoOutcome, VideoScope } from "./queries";

export const HISTORY_OUTCOMES: HistoryOutcome[] = [
	"all",
	"failed",
	"cancelled",
];
export const HISTORY_MEDIA_SCOPES: HistoryMedia[] = [
	"any",
	"on_disk",
	"removed",
];

export function isHistoryMedia(value: unknown): value is HistoryMedia {
	return HISTORY_MEDIA_SCOPES.includes(value as HistoryMedia);
}

export function validateHistorySearch(
	search: Record<string, unknown>,
): HistoryView {
	return {
		outcome: HISTORY_OUTCOMES.includes(search.outcome as HistoryOutcome)
			? (search.outcome as HistoryOutcome)
			: "all",
		media: isHistoryMedia(search.media) ? search.media : "any",
	};
}

const OUTCOME_PARAM: Record<HistoryOutcome, VideoOutcome | undefined> = {
	all: undefined,
	failed: "failed",
	cancelled: "cancelled",
};
const SCOPE_BY_MEDIA: Record<HistoryMedia, VideoScope> = {
	any: "all",
	on_disk: "active",
	removed: "removed",
};

export function historyFilters(view: HistoryView) {
	return {
		outcome: OUTCOME_PARAM[view.outcome],
		scope: SCOPE_BY_MEDIA[view.media],
	};
}

export function historyEmptyKey(view: HistoryView): string {
	if (view.media === "removed") return "history.empty_removed";
	if (view.media === "on_disk" && view.outcome === "all")
		return "history.empty_on_disk";
	return `history.empty_${view.outcome}`;
}

export function historyTabCounts(
	data: HistoryCountsResponse | undefined,
	media: HistoryMedia,
): Record<HistoryOutcome, number | undefined> {
	if (!data) return { all: undefined, failed: undefined, cancelled: undefined };
	const scoped = (counts: { on_disk: number; removed: number }) =>
		media === "any" ? counts.on_disk + counts.removed : counts[media];
	return {
		all: scoped(data.all),
		failed: scoped(data.failed),
		cancelled: scoped(data.cancelled),
	};
}
