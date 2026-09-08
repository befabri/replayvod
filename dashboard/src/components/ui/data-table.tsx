import { CaretDownIcon, CaretUpIcon } from "@phosphor-icons/react";
import {
	type ColumnDef,
	flexRender,
	getCoreRowModel,
	getSortedRowModel,
	type OnChangeFn,
	type Row,
	type RowSelectionState,
	type SortingState,
	useReactTable,
} from "@tanstack/react-table";
import { useWindowVirtualizer } from "@tanstack/react-virtual";
import { useCallback, useEffect, useRef, useState } from "react";
import {
	Table,
	TableBody,
	TableCell,
	TableHead,
	TableHeader,
	TableRow,
} from "./table";

// DataTable is the shared TanStack Table wrapper used by the dashboard
// system pages. Headless by design so each caller supplies its own
// column definitions; sorting is opt-in per column via `enableSorting`.
//
// Sorting is client-side by default (the table reorders `data`). Pass
// `sorting` + `onSortingChange` + `manualSorting` to control it externally and
// have the header drive a server-side sort instead — the caller re-fetches
// already-sorted data rather than the table reordering a single page.
export function DataTable<TData, TValue>({
	columns,
	data,
	emptyMessage,
	virtualizeRows = false,
	estimateRowHeight = 74,
	overscan = 8,
	sorting: controlledSorting,
	onSortingChange,
	manualSorting = false,
	rowSelection: controlledRowSelection,
	onRowSelectionChange,
	enableRowSelection,
	getRowId,
}: {
	columns: ColumnDef<TData, TValue>[];
	data: TData[];
	emptyMessage?: string;
	virtualizeRows?: boolean;
	estimateRowHeight?: number;
	overscan?: number;
	sorting?: SortingState;
	onSortingChange?: OnChangeFn<SortingState>;
	manualSorting?: boolean;
	// Row selection is the table's own state, so a select column reads it off
	// `row`/`table` in its cell rather than closing over it. That keeps the
	// column definitions stable across a click instead of rebuilding them.
	// Pass `getRowId` alongside it so a selection survives the rows moving
	// (a sort, a page appended) instead of being keyed by index.
	rowSelection?: RowSelectionState;
	onRowSelectionChange?: OnChangeFn<RowSelectionState>;
	enableRowSelection?: boolean | ((row: Row<TData>) => boolean);
	getRowId?: (originalRow: TData, index: number, parent?: Row<TData>) => string;
}) {
	const [internalSorting, setInternalSorting] = useState<SortingState>([]);
	const sorting = controlledSorting ?? internalSorting;
	const [internalRowSelection, setInternalRowSelection] =
		useState<RowSelectionState>({});
	const rowSelection = controlledRowSelection ?? internalRowSelection;
	const bodyRef = useRef<HTMLTableSectionElement | null>(null);
	const [scrollMargin, setScrollMargin] = useState(0);

	const table = useReactTable({
		data,
		columns,
		getRowId,
		state: { sorting, rowSelection },
		onSortingChange: onSortingChange ?? setInternalSorting,
		onRowSelectionChange: onRowSelectionChange ?? setInternalRowSelection,
		enableRowSelection,
		manualSorting,
		// A server-sorted table has no unsorted state to return to: clearing the
		// sort just falls back to whatever default the query uses, under a header
		// that no longer shows which. Cycle desc/asc instead.
		enableSortingRemoval: !manualSorting,
		getCoreRowModel: getCoreRowModel(),
		getSortedRowModel: getSortedRowModel(),
	});
	const rows = table.getRowModel().rows;

	const updateScrollMargin = useCallback(() => {
		const node = bodyRef.current;
		if (!node) return;
		const next = node.getBoundingClientRect().top + window.scrollY;
		setScrollMargin((prev) => (Math.abs(prev - next) < 1 ? prev : next));
	}, []);

	useEffect(updateScrollMargin);

	useEffect(() => {
		window.addEventListener("resize", updateScrollMargin);
		return () => window.removeEventListener("resize", updateScrollMargin);
	}, [updateScrollMargin]);

	const rowVirtualizer = useWindowVirtualizer<HTMLTableRowElement>({
		count: rows.length,
		enabled: virtualizeRows,
		estimateSize: () => estimateRowHeight,
		getItemKey: (index) => rows[index]?.id ?? index,
		overscan,
		scrollMargin,
	});

	// biome-ignore lint/correctness/useExhaustiveDependencies: sorting/filtering changes row order and invalidates measured row heights.
	useEffect(() => {
		rowVirtualizer.measure();
	}, [rows.length, sorting, rowVirtualizer]);

	const virtualRows = virtualizeRows ? rowVirtualizer.getVirtualItems() : [];
	const firstVirtualRow = virtualRows[0];
	const lastVirtualRow = virtualRows[virtualRows.length - 1];
	const virtualPaddingTop = firstVirtualRow
		? Math.max(0, firstVirtualRow.start - scrollMargin)
		: 0;
	const virtualPaddingBottom = lastVirtualRow
		? Math.max(
				0,
				rowVirtualizer.getTotalSize() - (lastVirtualRow.end - scrollMargin),
			)
		: 0;
	const visibleRows = virtualizeRows
		? virtualRows
				.map((virtualRow) => ({
					virtualRow,
					row: rows[virtualRow.index],
				}))
				.filter((item): item is typeof item & { row: (typeof rows)[number] } =>
					Boolean(item.row),
				)
		: rows.map((row) => ({ virtualRow: null, row }));

	return (
		<Table>
			<TableHeader>
				{table.getHeaderGroups().map((headerGroup) => (
					<TableRow key={headerGroup.id}>
						{headerGroup.headers.map((header) => {
							const sort = header.column.getIsSorted();
							const canSort = header.column.getCanSort();
							return (
								<TableHead key={header.id}>
									{header.isPlaceholder ? null : canSort ? (
										<button
											type="button"
											onClick={header.column.getToggleSortingHandler()}
											className="inline-flex items-center gap-1 uppercase hover:text-foreground"
										>
											{flexRender(
												header.column.columnDef.header,
												header.getContext(),
											)}
											{sort === "asc" ? (
												<CaretUpIcon className="size-3" />
											) : sort === "desc" ? (
												<CaretDownIcon className="size-3" />
											) : null}
										</button>
									) : (
										flexRender(
											header.column.columnDef.header,
											header.getContext(),
										)
									)}
								</TableHead>
							);
						})}
					</TableRow>
				))}
			</TableHeader>
			<TableBody ref={bodyRef}>
				{rows.length === 0 ? (
					<TableRow>
						<TableCell
							colSpan={columns.length}
							className="h-24 text-center text-muted-foreground"
						>
							{emptyMessage ?? "No results."}
						</TableCell>
					</TableRow>
				) : (
					<>
						{virtualPaddingTop > 0 && (
							<TableSpacerRow
								colSpan={columns.length}
								height={virtualPaddingTop}
							/>
						)}
						{visibleRows.map(({ row, virtualRow }) => (
							<TableRow
								key={row.id}
								data-index={virtualRow?.index}
								data-state={row.getIsSelected() ? "selected" : undefined}
								ref={virtualizeRows ? rowVirtualizer.measureElement : undefined}
							>
								{row.getVisibleCells().map((cell) => (
									<TableCell key={cell.id}>
										{flexRender(cell.column.columnDef.cell, cell.getContext())}
									</TableCell>
								))}
							</TableRow>
						))}
						{virtualPaddingBottom > 0 && (
							<TableSpacerRow
								colSpan={columns.length}
								height={virtualPaddingBottom}
							/>
						)}
					</>
				)}
			</TableBody>
		</Table>
	);
}

function TableSpacerRow({
	colSpan,
	height,
}: {
	colSpan: number;
	height: number;
}) {
	return (
		<TableRow aria-hidden="true" className="border-0 hover:bg-transparent">
			<TableCell colSpan={colSpan} className="p-0" style={{ height }} />
		</TableRow>
	);
}
