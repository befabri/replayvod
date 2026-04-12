import {
	type ColumnDef,
	type SortingState,
	flexRender,
	getCoreRowModel,
	getSortedRowModel,
	useReactTable,
} from "@tanstack/react-table"
import { CaretDown, CaretUp } from "@phosphor-icons/react"
import { useState } from "react"
import {
	Table,
	TableBody,
	TableCell,
	TableHead,
	TableHeader,
	TableRow,
} from "./table"

// DataTable is the shared TanStack Table wrapper used by the dashboard
// system pages. Headless by design so each caller supplies its own
// column definitions; sorting is client-side and opt-in per column via
// `enableSorting` in the ColumnDef.
export function DataTable<TData, TValue>({
	columns,
	data,
	emptyMessage,
}: {
	columns: ColumnDef<TData, TValue>[]
	data: TData[]
	emptyMessage?: string
}) {
	const [sorting, setSorting] = useState<SortingState>([])

	const table = useReactTable({
		data,
		columns,
		state: { sorting },
		onSortingChange: setSorting,
		getCoreRowModel: getCoreRowModel(),
		getSortedRowModel: getSortedRowModel(),
	})

	return (
		<Table>
			<TableHeader>
				{table.getHeaderGroups().map((headerGroup) => (
					<TableRow key={headerGroup.id}>
						{headerGroup.headers.map((header) => {
							const sort = header.column.getIsSorted()
							const canSort = header.column.getCanSort()
							return (
								<TableHead key={header.id}>
									{header.isPlaceholder ? null : canSort ? (
										<button
											type="button"
											onClick={header.column.getToggleSortingHandler()}
											className="inline-flex items-center gap-1 hover:text-foreground"
										>
											{flexRender(
												header.column.columnDef.header,
												header.getContext(),
											)}
											{sort === "asc" ? (
												<CaretUp className="size-3" />
											) : sort === "desc" ? (
												<CaretDown className="size-3" />
											) : null}
										</button>
									) : (
										flexRender(
											header.column.columnDef.header,
											header.getContext(),
										)
									)}
								</TableHead>
							)
						})}
					</TableRow>
				))}
			</TableHeader>
			<TableBody>
				{table.getRowModel().rows.length === 0 ? (
					<TableRow>
						<TableCell
							colSpan={columns.length}
							className="h-24 text-center text-muted-foreground"
						>
							{emptyMessage ?? "No results."}
						</TableCell>
					</TableRow>
				) : (
					table.getRowModel().rows.map((row) => (
						<TableRow key={row.id}>
							{row.getVisibleCells().map((cell) => (
								<TableCell key={cell.id}>
									{flexRender(cell.column.columnDef.cell, cell.getContext())}
								</TableCell>
							))}
						</TableRow>
					))
				)}
			</TableBody>
		</Table>
	)
}
