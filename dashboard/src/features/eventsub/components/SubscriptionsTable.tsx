import { useMemo } from "react";
import { useTranslation } from "react-i18next";
import { DataTable } from "@/components/ui/data-table";
import { type SubRowData, subscriptionColumns } from "./columns";

export function SubscriptionsTable({
	rows,
	loading = false,
}: {
	rows: SubRowData[];
	loading?: boolean;
}) {
	const { t } = useTranslation();
	const columns = useMemo(() => subscriptionColumns(t), [t]);
	return (
		<DataTable
			columns={columns}
			data={rows}
			loading={loading}
			emptyMessage={t("eventsub.no_subscriptions")}
		/>
	);
}
