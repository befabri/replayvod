import { FunnelSimple } from "@phosphor-icons/react";
import { createFileRoute } from "@tanstack/react-router";
import { useEffect, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { TitledLayout } from "@/components/layout/titled-layout";
import { Button } from "@/components/ui/button";
import { DataTable } from "@/components/ui/data-table";
import {
	DropdownMenu,
	DropdownMenuContent,
	DropdownMenuItem,
	DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { useInfiniteVideoPages } from "@/features/videos";
import { historyColumns } from "@/features/videos/components/activityColumns";

const PAGE_SIZE = 50;
type StatusKey = "DONE" | "FAILED";
const STATUS_KEYS: StatusKey[] = ["DONE", "FAILED"];

export const Route = createFileRoute("/dashboard/activity/history")({
	validateSearch: (search: Record<string, unknown>) => ({
		status:
			search.status === "FAILED" || search.status === "DONE"
				? (search.status as StatusKey)
				: ("DONE" as const),
	}),
	component: HistoryPage,
});

function HistoryPage() {
	const { t } = useTranslation();
	const { status } = Route.useSearch();
	const navigate = Route.useNavigate();
	const [page, setPage] = useState(0);
	// latestStatus is read inside the async fetchNextPage().then
	// callback below to discard a paginator advance when the user
	// changed the status filter during the fetch. Only used there.
	const latestStatus = useRef(status);
	latestStatus.current = status;
	const videos = useInfiniteVideoPages(PAGE_SIZE, status, "created_at", "desc");
	const pages = videos.data?.pages ?? [];
	const currentPage = pages[page]?.items ?? [];
	const canGoPrev = page > 0;
	const canGoNext = page < pages.length - 1 || !!videos.hasNextPage;

	// Status is the intentional re-run trigger — when the query's
	// cache key rotates, the paginator position is reset.
	// biome-ignore lint/correctness/useExhaustiveDependencies: status is the reset trigger, not a read
	useEffect(() => {
		setPage(0);
	}, [status]);

	return (
		<TitledLayout
			title={t("history.title")}
			actions={
				<StatusDropdown
					current={status}
					onChange={(next) => {
						setPage(0);
						void navigate({ search: { status: next } });
					}}
				/>
			}
		>
			<p className="text-muted-foreground mb-6 -mt-6">
				{t("history.description")}
			</p>

			{videos.isLoading && (
				<div className="text-muted-foreground">{t("common.loading")}</div>
			)}
			{videos.error && (
				<div className="rounded-lg bg-destructive/10 p-4 text-destructive text-sm shadow-sm">
					{t("history.failed_to_load")}: {videos.error.message}
				</div>
			)}
			{!videos.isLoading && !videos.error && (
				<>
					<DataTable
						columns={historyColumns}
						data={currentPage}
						emptyMessage={t("history.empty")}
					/>
					<div className="flex items-center gap-2 mt-4">
						<Button
							variant="outline"
							size="sm"
							disabled={!canGoPrev}
							onClick={() => setPage((p) => Math.max(0, p - 1))}
						>
							{t("videos.previous")}
						</Button>
						<span className="text-sm text-muted-foreground">
							{t("videos.page", { n: page + 1 })}
						</span>
						<Button
							variant="outline"
							size="sm"
							disabled={!canGoNext || videos.isFetchingNextPage}
							onClick={() => {
								const nextPage = page + 1;
								const requestedStatus = status;
								if (page < pages.length - 1) {
									setPage(nextPage);
									return;
								}
								if (!videos.hasNextPage) {
									return;
								}
								void videos.fetchNextPage().then((result) => {
									if (
										!result.error &&
										latestStatus.current === requestedStatus
									) {
										setPage(nextPage);
									}
								});
							}}
						>
							{t("videos.next")}
						</Button>
					</div>
				</>
			)}
		</TitledLayout>
	);
}

function StatusDropdown({
	current,
	onChange,
}: {
	current: StatusKey;
	onChange: (next: StatusKey) => void;
}) {
	const { t } = useTranslation();
	const labels: Record<StatusKey, string> = {
		DONE: t("history.filter_done"),
		FAILED: t("history.filter_failed"),
	};
	return (
		<DropdownMenu>
			<DropdownMenuTrigger
				render={(triggerProps) => (
					<Button variant="outline" size="sm" {...triggerProps}>
						<FunnelSimple className="size-4" />
						{labels[current]}
					</Button>
				)}
			/>
			<DropdownMenuContent>
				{STATUS_KEYS.map((key) => (
					<DropdownMenuItem key={key} onClick={() => onChange(key)}>
						{labels[key]}
					</DropdownMenuItem>
				))}
			</DropdownMenuContent>
		</DropdownMenu>
	);
}
