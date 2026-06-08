import type { ColumnDef } from "@tanstack/react-table";
import { useTranslation } from "react-i18next";
import { DataTable } from "./data-table";

type QueryTableState<TData> = {
	data: TData | undefined;
	isLoading: boolean;
	error: { message: string } | null;
};

// Shared loading/error/empty shell around DataTable. Surrounding chrome stays in
// the route; getRows pulls the row array from the payload (identity, or .data for
// an envelope).
export function QueryTable<TData, Row, TValue>({
	query,
	columns,
	getRows,
	emptyMessage,
	errorLabel,
}: {
	query: QueryTableState<TData>;
	columns: ColumnDef<Row, TValue>[];
	getRows: (data: TData) => Row[];
	emptyMessage: string;
	errorLabel: string;
}) {
	const { t } = useTranslation();
	return (
		<>
			{query.isLoading && (
				<div className="text-muted-foreground">{t("common.loading")}</div>
			)}
			{query.error && (
				<div className="rounded-md bg-destructive/10 border border-destructive/20 p-4 text-destructive text-sm">
					{errorLabel}: {query.error.message}
				</div>
			)}
			{query.data !== undefined && (
				<DataTable
					columns={columns}
					data={getRows(query.data)}
					emptyMessage={emptyMessage}
				/>
			)}
		</>
	);
}
