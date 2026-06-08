import { createFileRoute } from "@tanstack/react-router";
import { useMemo } from "react";
import { useTranslation } from "react-i18next";
import { TitledLayout } from "@/components/layout/titled-layout";
import { QueryTable } from "@/components/ui/query-table";
import { useSessions } from "@/features/sessions";
import { sessionColumns } from "@/features/sessions/components/columns";

export const Route = createFileRoute("/dashboard/sessions")({
	component: SessionsPage,
});

function SessionsPage() {
	const { t } = useTranslation();
	const sessions = useSessions();
	const columns = useMemo(() => sessionColumns(t), [t]);

	return (
		<TitledLayout title={t("sessions.title")}>
			<p className="text-muted-foreground mb-6 -mt-6">
				{t("sessions.description")}
			</p>

			<QueryTable
				query={sessions}
				columns={columns}
				getRows={(data) => data}
				emptyMessage={t("sessions.empty")}
				errorLabel={t("sessions.failed_to_load")}
			/>
		</TitledLayout>
	);
}
