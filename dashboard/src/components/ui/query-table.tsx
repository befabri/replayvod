import type { ColumnDef } from "@tanstack/react-table";
import { Alert } from "./alert";
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
	return (
		<>
			{query.error && (
				<Alert variant="destructive">
					{errorLabel}: {query.error.message}
				</Alert>
			)}
			{(query.isLoading || query.data !== undefined) && (
				<DataTable
					columns={columns}
					data={query.data === undefined ? [] : getRows(query.data)}
					getRowId={getRowId}
					emptyMessage={emptyMessage}
					loading={query.isLoading}
				/>
			)}
		</>
	);
}
