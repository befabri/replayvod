import { useMemo } from "react";
import { useTranslation } from "react-i18next";
import type { useMyScheduleRequests } from "@/features/requests";
import { myRequestColumns } from "@/features/requests/components/columns";
import { RequestTable } from "@/features/requests/components/RequestTable";

export function MyRequests({
	requests,
}: {
	requests: ReturnType<typeof useMyScheduleRequests>;
}) {
	const { t } = useTranslation();
	const columns = useMemo(() => myRequestColumns(t), [t]);

	return (
		<section className="mb-8">
			<h2 className="mb-3 text-lg font-semibold">{t("requests.mine_title")}</h2>
			<RequestTable
				query={requests}
				columns={columns}
				emptyMessage={t("requests.empty")}
				errorLabel={t("requests.failed_to_load")}
			/>
		</section>
	);
}
