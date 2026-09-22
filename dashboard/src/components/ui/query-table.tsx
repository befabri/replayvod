import type { ColumnDef } from "@tanstack/react-table";
import { useTranslation } from "react-i18next";
import { DataTable } from "./data-table";

type QueryTableState<TData> = {
	data: TData | undefined;
	isLoading: boolean;
	error: { message: string } | null;
};

export function QueryTable<TData, Row, TValue>({
	query,
	columns,
	getRows,
	getRowId,
	emptyMessage,
	errorLabel,
}: {
	query: QueryTableState<TData>;
	columns: ColumnDef<Row, TValue>[];
	getRows: (data: TData) => Row[];
	getRowId?: (row: Row, index: number) => string;
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
					getRowId={getRowId}
					emptyMessage={emptyMessage}
				/>
			)}
		</>
	);
}
