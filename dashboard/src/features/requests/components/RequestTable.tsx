import type { ColumnDef } from "@tanstack/react-table";
import { useTranslation } from "react-i18next";
import { Button } from "@/components/ui/button";
import { QueryTable } from "@/components/ui/query-table";
import type { ScheduleRequestResponse } from "@/features/requests";
import type { useMyScheduleRequests } from "@/features/requests/queries";

export function RequestTable({
	query,
	columns,
	emptyMessage,
	errorLabel,
}: {
	query: ReturnType<typeof useMyScheduleRequests>;
	columns: ColumnDef<ScheduleRequestResponse>[];
	emptyMessage: string;
	errorLabel: string;
}) {
	const { t } = useTranslation();
	return (
		<>
			<QueryTable
				query={query}
				columns={columns}
				getRows={(rows) => rows}
				emptyMessage={emptyMessage}
				errorLabel={errorLabel}
			/>
			{query.hasNextPage && (
				<Button
					variant="outline"
					className="mt-3"
					disabled={query.isFetchingNextPage}
					onClick={() => {
						void query.fetchNextPage();
					}}
				>
					{query.isFetchingNextPage
						? t("common.loading")
						: t("common.show_more")}
				</Button>
			)}
		</>
	);
}
