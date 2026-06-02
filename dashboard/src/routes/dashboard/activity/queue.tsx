import { createFileRoute } from "@tanstack/react-router";
import { useMemo } from "react";
import { useTranslation } from "react-i18next";
import { TitledLayout } from "@/components/layout/titled-layout";
import { DataTable } from "@/components/ui/data-table";
import { useVideoListPage } from "@/features/videos";
import { queueColumns } from "@/features/videos/components/activityColumns";

export const Route = createFileRoute("/dashboard/activity/queue")({
	component: QueuePage,
});

function QueuePage() {
	const { t } = useTranslation();
	const running = useVideoListPage(50, "RUNNING", "created_at", "asc");
	const pending = useVideoListPage(50, "PENDING", "created_at", "asc");
	const columns = useMemo(() => queueColumns(t), [t]);

	const rows = [...(running.data?.items ?? []), ...(pending.data?.items ?? [])];
	const loading = running.isLoading || pending.isLoading;
	const error = running.error ?? pending.error;

	return (
		<TitledLayout title={t("queue.title")}>
			<p className="text-muted-foreground mb-6 -mt-6">
				{t("queue.description")}
			</p>

			{loading && (
				<div className="text-muted-foreground">{t("common.loading")}</div>
			)}
			{error && (
				<div className="rounded-md bg-destructive/10 border border-destructive/20 p-4 text-destructive text-sm">
					{t("queue.failed_to_load")}: {error.message}
				</div>
			)}
			{!loading && !error && (
				<DataTable
					columns={columns}
					data={rows}
					emptyMessage={t("queue.empty")}
				/>
			)}
		</TitledLayout>
	);
}
