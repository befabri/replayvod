import { createFileRoute } from "@tanstack/react-router";
import { useMemo } from "react";
import { useTranslation } from "react-i18next";
import { TitledLayout } from "@/components/layout/titled-layout";
import { DataTable } from "@/components/ui/data-table";
import { useSessions } from "@/features/sessions";
import { sessionColumns } from "@/features/sessions/components/columns";

export const Route = createFileRoute("/dashboard/sessions")({
	component: SessionsPage,
});

function SessionsPage() {
	const { t } = useTranslation();
	const { data, isLoading, error } = useSessions();
	const columns = useMemo(() => sessionColumns(t), [t]);

	return (
		<TitledLayout title={t("sessions.title")}>
			<p className="text-muted-foreground mb-6 -mt-6">
				{t("sessions.description")}
			</p>

			{isLoading && (
				<div className="text-muted-foreground">{t("common.loading")}</div>
			)}
			{error && (
				<div className="rounded-md bg-destructive/10 border border-destructive/20 p-4 text-destructive text-sm">
					{t("sessions.failed_to_load")}: {error.message}
				</div>
			)}

			{data && (
				<DataTable
					columns={columns}
					data={data}
					emptyMessage={t("sessions.empty")}
				/>
			)}
		</TitledLayout>
	);
}
