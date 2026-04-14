import { FunnelSimple } from "@phosphor-icons/react";
import { createFileRoute } from "@tanstack/react-router";
import { useState } from "react";
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
import { useVideos } from "@/features/videos";
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
	const { data, isLoading, error } = useVideos(
		PAGE_SIZE,
		page * PAGE_SIZE,
		status,
	);

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

			{isLoading && (
				<div className="text-muted-foreground">{t("common.loading")}</div>
			)}
			{error && (
				<div className="rounded-lg bg-destructive/10 p-4 text-destructive text-sm shadow-sm">
					{t("history.failed_to_load")}: {error.message}
				</div>
			)}
			{!isLoading && !error && (
				<>
					<DataTable
						columns={historyColumns}
						data={data ?? []}
						emptyMessage={t("history.empty")}
					/>
					<div className="flex items-center gap-2 mt-4">
						<Button
							variant="outline"
							size="sm"
							disabled={page === 0}
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
							disabled={(data?.length ?? 0) < PAGE_SIZE}
							onClick={() => setPage((p) => p + 1)}
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
