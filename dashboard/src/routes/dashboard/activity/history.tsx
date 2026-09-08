import { createFileRoute } from "@tanstack/react-router";
import type { OnChangeFn, SortingState } from "@tanstack/react-table";
import { useEffect, useMemo, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { TitledLayout } from "@/components/layout/titled-layout";
import { DataTable } from "@/components/ui/data-table";
import { FilterTabs } from "@/components/ui/filter-tabs";
import { Pager } from "@/components/ui/pager";
import { ToggleGroup, ToggleGroupItem } from "@/components/ui/toggle-group";
import {
	useHistoryCounts,
	useInfiniteVideoPages,
	type VideoOrder,
	type VideoSort,
} from "@/features/videos";
import {
	HISTORY_SORT_BY_COLUMN,
	type HistoryMedia,
	type HistoryOutcome,
	type HistoryView,
	historyColumns,
} from "@/features/videos/components/activityColumns";
import {
	HISTORY_MEDIA_SCOPES,
	HISTORY_OUTCOMES,
	historyEmptyKey,
	historyFilters,
	historyTabCounts,
	isHistoryMedia,
	validateHistorySearch,
} from "@/features/videos/history";
import { useCanManageVideos } from "@/features/videos/permissions";

const PAGE_SIZE = 50;
// Newest first by default. The "when" column maps to the server's history_when
// sort (see HISTORY_SORT_BY_COLUMN).
const DEFAULT_SORTING: SortingState = [{ id: "when", desc: true }];

type HistoryQueryIdentity = {
	outcome: HistoryOutcome;
	media: HistoryMedia;
	sortKey: VideoSort;
	order: VideoOrder;
};

function historyQuerySignature(identity: HistoryQueryIdentity): string {
	return [
		identity.outcome,
		identity.media,
		identity.sortKey,
		identity.order,
	].join("|");
}

export const Route = createFileRoute("/dashboard/activity/history")({
	validateSearch: validateHistorySearch,
	component: HistoryPage,
});

function HistoryPage() {
	const { outcome, media } = Route.useSearch();
	const navigate = Route.useNavigate();
	return (
		<HistoryContent
			key={`${outcome}|${media}`}
			view={{ outcome, media }}
			onViewChange={(next) => {
				void navigate({ search: next });
			}}
		/>
	);
}

function HistoryContent({
	view,
	onViewChange,
}: {
	view: HistoryView;
	onViewChange: (next: HistoryView) => void;
}) {
	const { t, i18n } = useTranslation();
	const [page, setPage] = useState(0);
	const [sorting, setSorting] = useState<SortingState>(DEFAULT_SORTING);

	// Drive a real server-side sort from the table header. The column id maps to
	// a VideoSort key; unsortable columns never reach here (enableSorting gates
	// them), so the fallback is just defensive.
	const activeSort = sorting[0];
	const sortKey: VideoSort = activeSort
		? (HISTORY_SORT_BY_COLUMN[activeSort.id] ?? "created_at")
		: "created_at";
	const order: VideoOrder = activeSort && !activeSort.desc ? "asc" : "desc";
	const querySignature = historyQuerySignature({ ...view, sortKey, order });
	const videos = useInfiniteVideoPages(PAGE_SIZE, undefined, sortKey, order, {
		...historyFilters(view),
		terminalOnly: true,
	});
	const loadedPages = videos.data?.pages ?? [];
	// Resolve the delete permission once for the table; the actions column omits
	// the remove control for viewers instead of mounting a per-row hook stack
	// that renders null.
	const canManage = useCanManageVideos();
	const columns = useMemo(
		() => historyColumns(t, view, canManage, i18n.language),
		[t, view, canManage, i18n.language],
	);
	const counts = useHistoryTabCounts(view.media);

	// The async fetchNextPage().then must only advance the page for the exact
	// query that requested it. Sorting and view changes both rotate the query
	// key; a stale completion from the previous key must not mutate the current
	// paginator.
	const latestQuerySignature = useRef(querySignature);
	latestQuerySignature.current = querySignature;

	// A header click changes the server sort, so jump back to the first page.
	const handleSortingChange: OnChangeFn<SortingState> = (updater) => {
		setSorting((prev) =>
			typeof updater === "function" ? updater(prev) : updater,
		);
		setPage(0);
	};

	// Keep the page index inside the loaded range. A delete/invalidation can
	// shrink the cached pages under a deep page; clamp so the table doesn't
	// strand the user on an empty page.
	useEffect(() => {
		if (!videos.hasNextPage && page > loadedPages.length - 1) {
			setPage(Math.max(0, loadedPages.length - 1));
		}
	}, [loadedPages.length, page, videos.hasNextPage]);

	const current = loadedPages[page]?.items ?? [];
	const canNext =
		(page < loadedPages.length - 1 || !!videos.hasNextPage) &&
		!videos.isFetchingNextPage;

	// Cursor pagination: advancing past the loaded pages fetches the next one,
	// then moves on only if the fetch succeeded and the view didn't change.
	const goNext = () => {
		const next = page + 1;
		if (page < loadedPages.length - 1) {
			setPage(next);
			return;
		}
		if (!videos.hasNextPage) {
			return;
		}
		const requested = querySignature;
		void videos.fetchNextPage().then((result) => {
			if (!result.error && latestQuerySignature.current === requested) {
				setPage(next);
			}
		});
	};

	return (
		<TitledLayout title={t("history.title")}>
			<p className="text-muted-foreground mb-6 -mt-6">
				{t("history.description")}
			</p>

			<div className="flex flex-wrap items-end justify-between gap-x-6 gap-y-3">
				<FilterTabs
					value={view.outcome}
					onChange={(next) => {
						onViewChange({ ...view, outcome: next as HistoryOutcome });
					}}
					options={HISTORY_OUTCOMES.map((key) => ({
						value: key,
						label: t(`history.outcome_${key}`),
						count: counts[key],
					}))}
				/>
				<div className="flex items-center gap-2 pb-3">
					<span className="text-xs text-muted-foreground">
						{t("history.media_label")}
					</span>
					<ToggleGroup
						value={[view.media]}
						onValueChange={(next) => {
							const media = next[0];
							if (isHistoryMedia(media)) {
								onViewChange({ ...view, media });
							}
						}}
					>
						{HISTORY_MEDIA_SCOPES.map((scope) => (
							<ToggleGroupItem key={scope} value={scope}>
								{t(`history.scope_${scope}`)}
							</ToggleGroupItem>
						))}
					</ToggleGroup>
				</div>
			</div>

			{videos.isLoading && (
				<div className="mt-6 text-muted-foreground">{t("common.loading")}</div>
			)}
			{videos.error && (
				<div className="mt-6 rounded-lg bg-destructive/10 p-4 text-destructive text-sm shadow-sm">
					{t("history.failed_to_load")}: {videos.error.message}
				</div>
			)}
			{!videos.isLoading && !videos.error && (
				<div className="mt-6">
					<DataTable
						columns={columns}
						data={current}
						emptyMessage={t(historyEmptyKey(view))}
						sorting={sorting}
						onSortingChange={handleSortingChange}
						manualSorting
					/>
					<Pager
						page={page}
						total={counts[view.outcome]}
						hasNext={canNext}
						onPrev={() => setPage((p) => Math.max(0, p - 1))}
						onNext={goNext}
					/>
				</div>
			)}
		</TitledLayout>
	);
}

// useHistoryTabCounts labels each outcome tab under the current media scope.
// The server returns both halves of every outcome, so changing scope re-labels
// the tabs from cache instead of refetching.
function useHistoryTabCounts(
	media: HistoryMedia,
): Record<HistoryOutcome, number | undefined> {
	const { data } = useHistoryCounts();
	return useMemo(() => historyTabCounts(data, media), [data, media]);
}
