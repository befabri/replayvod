import { createFileRoute } from "@tanstack/react-router";
import type { OnChangeFn, SortingState } from "@tanstack/react-table";
import { useEffect, useMemo, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { TitledLayout } from "@/components/layout/titled-layout";
import { Alert } from "@/components/ui/alert";
import { DataTable } from "@/components/ui/data-table";
import { Pager } from "@/components/ui/pager";
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
import { HistoryViewControls } from "@/features/videos/components/HistoryViewControls";
import {
	historyEmptyKey,
	historyFilters,
	historyTabCounts,
	validateHistorySearch,
} from "@/features/videos/history";
import { useCanManageVideos } from "@/features/videos/permissions";

const PAGE_SIZE = 50;
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
	const canManage = useCanManageVideos();
	const columns = useMemo(
		() => historyColumns(t, view, canManage, i18n.language),
		[t, view, canManage, i18n.language],
	);
	const counts = useHistoryTabCounts(view.media);

	const latestQuerySignature = useRef(querySignature);
	latestQuerySignature.current = querySignature;

	const handleSortingChange: OnChangeFn<SortingState> = (updater) => {
		setSorting((prev) =>
			typeof updater === "function" ? updater(prev) : updater,
		);
		setPage(0);
	};

	useEffect(() => {
		if (!videos.hasNextPage && page > loadedPages.length - 1) {
			setPage(Math.max(0, loadedPages.length - 1));
		}
	}, [loadedPages.length, page, videos.hasNextPage]);

	const current = loadedPages[page]?.items ?? [];
	const canNext =
		(page < loadedPages.length - 1 || !!videos.hasNextPage) &&
		!videos.isFetchingNextPage;

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

			<HistoryViewControls
				view={view}
				counts={counts}
				onViewChange={onViewChange}
			/>

			{videos.error && (
				<Alert variant="destructive" className="mt-6">
					{t("history.failed_to_load")}: {videos.error.message}
				</Alert>
			)}
			{!videos.error && (
				<div className="mt-6">
					<DataTable
						columns={columns}
						data={current}
						loading={videos.isLoading}
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

function useHistoryTabCounts(
	media: HistoryMedia,
): Record<HistoryOutcome, number | undefined> {
	const { data } = useHistoryCounts();
	return useMemo(() => historyTabCounts(data, media), [data, media]);
}
