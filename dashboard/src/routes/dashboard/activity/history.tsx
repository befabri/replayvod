import { createFileRoute } from "@tanstack/react-router";
import type { OnChangeFn, SortingState } from "@tanstack/react-table";
import { useEffect, useMemo, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { TitledLayout } from "@/components/layout/titled-layout";
import { DataTable } from "@/components/ui/data-table";
import { FilterTabs } from "@/components/ui/filter-tabs";
import { Pager } from "@/components/ui/pager";
import {
	useInfiniteVideoPages,
	useStatistics,
	type VideoOrder,
	type VideoScope,
	type VideoSort,
} from "@/features/videos";
import {
	HISTORY_SORT_BY_COLUMN,
	type HistoryFilter,
	historyColumns,
} from "@/features/videos/components/activityColumns";
import { useCanManageVideos } from "@/features/videos/permissions";

const PAGE_SIZE = 50;
const FILTERS: HistoryFilter[] = ["all", "failed", "removed"];

function isFilter(value: unknown): value is HistoryFilter {
	return value === "all" || value === "failed" || value === "removed";
}

// Each filter maps to a (status, scope) pair. "all" is every recording incl
// tombstones; "failed" is live failures; "removed" is the tombstoned set.
const FILTER_QUERY: Record<
	HistoryFilter,
	{ status?: string; scope: VideoScope; terminalOnly: boolean }
> = {
	all: { status: undefined, scope: "all", terminalOnly: true },
	failed: { status: "FAILED", scope: "active", terminalOnly: true },
	removed: { status: undefined, scope: "removed", terminalOnly: true },
};

const EMPTY_KEY: Record<HistoryFilter, string> = {
	all: "history.empty_all",
	failed: "history.empty_failed",
	removed: "history.empty_removed",
};

// Newest first by default. The "when" column maps to the server's history_when
// sort (see HISTORY_SORT_BY_COLUMN).
const DEFAULT_SORTING: SortingState = [{ id: "when", desc: true }];

type HistoryQueryIdentity = {
	filter: HistoryFilter;
	status: string;
	scope: VideoScope;
	terminalOnly: boolean;
	sortKey: VideoSort;
	order: VideoOrder;
};

function historyQuerySignature(identity: HistoryQueryIdentity): string {
	return [
		identity.filter,
		identity.status,
		identity.scope,
		identity.terminalOnly ? "terminal" : "any",
		identity.sortKey,
		identity.order,
	].join("|");
}

export const Route = createFileRoute("/dashboard/activity/history")({
	validateSearch: (search: Record<string, unknown>) => ({
		filter: isFilter(search.filter) ? search.filter : "all",
	}),
	component: HistoryPage,
});

function HistoryPage() {
	const { filter } = Route.useSearch();
	const navigate = Route.useNavigate();
	return (
		<HistoryContent
			key={filter}
			filter={filter}
			onFilterChange={(next) => {
				void navigate({ search: { filter: next } });
			}}
		/>
	);
}

function HistoryContent({
	filter,
	onFilterChange,
}: {
	filter: HistoryFilter;
	onFilterChange: (next: HistoryFilter) => void;
}) {
	const { t, i18n } = useTranslation();
	const [page, setPage] = useState(0);
	const [sorting, setSorting] = useState<SortingState>(DEFAULT_SORTING);

	const { status, scope, terminalOnly } = FILTER_QUERY[filter];
	// Drive a real server-side sort from the table header. The column id maps to
	// a VideoSort key; unsortable columns never reach here (enableSorting gates
	// them), so the fallback is just defensive.
	const activeSort = sorting[0];
	const sortKey: VideoSort = activeSort
		? (HISTORY_SORT_BY_COLUMN[activeSort.id] ?? "created_at")
		: "created_at";
	const order: VideoOrder = activeSort && !activeSort.desc ? "asc" : "desc";
	const querySignature = historyQuerySignature({
		filter,
		status: status ?? "",
		scope,
		terminalOnly,
		sortKey,
		order,
	});
	const videos = useInfiniteVideoPages(PAGE_SIZE, status, sortKey, order, {
		scope,
		terminalOnly,
	});
	const loadedPages = videos.data?.pages ?? [];
	// Resolve the delete permission once for the table; the actions column omits
	// the remove control for viewers instead of mounting a per-row hook stack
	// that renders null.
	const canManage = useCanManageVideos();
	const columns = useMemo(
		() => historyColumns(t, filter, canManage, i18n.language),
		[t, filter, canManage, i18n.language],
	);
	const counts = useHistoryCounts();

	// The async fetchNextPage().then must only advance the page for the exact
	// query that requested it. Sorting and filter changes both rotate the query
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
	// then moves on only if the fetch succeeded and the tab didn't change.
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

			<FilterTabs
				value={filter}
				onChange={(next) => {
					onFilterChange(next as HistoryFilter);
				}}
				options={FILTERS.map((key) => ({
					value: key,
					label: t(`history.filter_${key}`),
					count: counts[key],
				}))}
			/>

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
						emptyMessage={t(EMPTY_KEY[filter])}
						sorting={sorting}
						onSortingChange={handleSortingChange}
						manualSorting
					/>
					<Pager
						page={page}
						total={counts[filter]}
						hasNext={canNext}
						onPrev={() => setPage((p) => Math.max(0, p - 1))}
						onNext={goNext}
					/>
				</div>
			)}
		</TitledLayout>
	);
}

// useHistoryCounts derives the per-tab counts from the statistics aggregate.
// by_status is live-only, so Failed comes from it and All adds the removed
// count; both are undefined until stats load (the tab then hides its count).
function useHistoryCounts(): Record<HistoryFilter, number | undefined> {
	const { data: stats } = useStatistics();
	return useMemo(() => {
		if (!stats) {
			return { all: undefined, failed: undefined, removed: undefined };
		}
		const activeTerminal = stats.by_status
			.filter((b) => b.status === "DONE" || b.status === "FAILED")
			.reduce((sum, b) => sum + b.count, 0);
		const failed =
			stats.by_status.find((b) => b.status === "FAILED")?.count ?? 0;
		return {
			all: activeTerminal + stats.removed,
			failed,
			removed: stats.removed,
		};
	}, [stats]);
}
