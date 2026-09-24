import type { ColumnDef } from "@tanstack/react-table";
import { DataTable } from "@/components/ui/data-table";

export function TableSkeletonComparison<TData, TValue>({
	columns,
	rows,
}: {
	columns: ColumnDef<TData, TValue>[];
	rows: TData[];
}) {
	return (
		<div className="grid gap-8">
			<div data-testid="skeleton">
				<DataTable
					columns={columns}
					data={[]}
					loading
					loadingRows={rows.length}
				/>
			</div>
			<div data-testid="loaded">
				<DataTable columns={columns} data={rows} />
			</div>
		</div>
	);
}

export function tableIn(root: Element): Element {
	const table = root.querySelector('[data-slot="table-container"]');
	if (!table) throw new Error("no table rendered");
	return table;
}
