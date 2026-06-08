import { createFileRoute } from "@tanstack/react-router";
import { useMemo } from "react";
import { useTranslation } from "react-i18next";
import { TitledLayout } from "@/components/layout/titled-layout";
import { QueryTable } from "@/components/ui/query-table";
import { useMyRequests } from "@/features/requests";
import { requestColumns } from "@/features/requests/components/columns";

export const Route = createFileRoute("/dashboard/requests")({
	component: RequestsPage,
});

function RequestsPage() {
	const { t } = useTranslation();
	const requests = useMyRequests();
	const columns = useMemo(() => requestColumns(t), [t]);

	return (
		<TitledLayout title={t("nav.requests")}>
			<QueryTable
				query={requests}
				columns={columns}
				getRows={(data) => data}
				emptyMessage={t("requests.empty")}
				errorLabel={t("requests.failed_to_load")}
			/>
		</TitledLayout>
	);
}
