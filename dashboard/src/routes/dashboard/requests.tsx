import { createFileRoute } from "@tanstack/react-router";
import { useMemo } from "react";
import { useTranslation } from "react-i18next";
import { TitledLayout } from "@/components/layout/titled-layout";
import { DataTable } from "@/components/ui/data-table";
import { useMyRequests } from "@/features/requests";
import { requestColumns } from "@/features/requests/components/columns";

export const Route = createFileRoute("/dashboard/requests")({
	component: RequestsPage,
});

function RequestsPage() {
	const { t } = useTranslation();
	const { data, isLoading, error } = useMyRequests();

	const columns = useMemo(() => requestColumns(t), [t]);

	return (
		<TitledLayout title={t("nav.requests")}>
			{isLoading && <div className="text-muted-foreground">Loading…</div>}

			{error && (
				<div className="rounded-md bg-destructive/10 border border-destructive/20 p-4 text-destructive text-sm">
					{error.message}
				</div>
			)}

			{data && (
				<DataTable
					columns={columns}
					data={data}
					emptyMessage={
						'No requests yet. Request a download by clicking "Request" on any video detail page.'
					}
				/>
			)}
		</TitledLayout>
	);
}
