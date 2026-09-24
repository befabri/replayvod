import { useMemo, useState } from "react";
import { useTranslation } from "react-i18next";
import type {
	ScheduleRequestResponse,
	useAllScheduleRequests,
} from "@/features/requests";
import { ApproveRequestDialog } from "@/features/requests/components/ApproveRequestDialog";
import { adminRequestColumns } from "@/features/requests/components/columns";
import { RequestTable } from "@/features/requests/components/RequestTable";

export function RequestsQueue({
	requests,
}: {
	requests: ReturnType<typeof useAllScheduleRequests>;
}) {
	const { t } = useTranslation();
	const [approving, setApproving] = useState<ScheduleRequestResponse | null>(
		null,
	);
	const columns = useMemo(() => adminRequestColumns(t, setApproving), [t]);

	if (requests.isSuccess && requests.data.length === 0) return null;

	return (
		<section className="mb-8">
			<h2 className="mb-3 text-lg font-semibold">
				{t("requests.queue_title")}
			</h2>
			<RequestTable
				query={requests}
				columns={columns}
				emptyMessage={t("requests.empty_queue")}
				errorLabel={t("requests.failed_to_load")}
			/>
			<ApproveRequestDialog
				request={approving}
				onClose={() => setApproving(null)}
			/>
		</section>
	);
}
