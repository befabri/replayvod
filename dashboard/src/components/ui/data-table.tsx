import { CaretDownIcon, CaretUpIcon } from "@phosphor-icons/react";
import {
	type ColumnDef,
	flexRender,
	getCoreRowModel,
	getSortedRowModel,
	type OnChangeFn,
	type Row,
	type RowData,
	type RowSelectionState,
	type SortingState,
	useReactTable,
} from "@tanstack/react-table";
import { useWindowVirtualizer } from "@tanstack/react-virtual";
import {
	type ReactNode,
	useCallback,
	useEffect,
	useRef,
	useState,
} from "react";
import { cn } from "@/lib/utils";
import { LoadingState } from "./loading-state";
import { Skeleton } from "./skeleton";
import {
	Table,
	TableBody,
	TableCell,
	TableHead,
	TableHeader,
	TableRow,
} from "./table";

declare module "@tanstack/react-table" {
	interface ColumnMeta<TData extends RowData, TValue> {
		skeleton?: ReactNode;
	}
}

const SKELETON_WIDTHS = ["w-3/4", "w-1/2", "w-2/3", "w-2/5", "w-3/5"];

const EMPTY_CELL_CLASS = "h-24 text-center text-muted-foreground";

export function DataTable<TData, TValue>({
	columns,
	data,
	emptyMessage,
	loading = false,
	loadingRows = 5,
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
	loading?: boolean;
	loadingRows?: number;
	virtualizeRows?: boolean;
	estimateRowHeight?: number;
	overscan?: number;
	sorting?: SortingState;
	onSortingChange?: OnChangeFn<SortingState>;
	manualSorting?: boolean;
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
		<>
			{loading ? <LoadingState className="sr-only" /> : null}
			<Table aria-busy={loading || undefined}>
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
					{loading && loadingRows === 0 ? (
						<TableRow aria-hidden="true" className="hover:bg-transparent">
							<TableCell colSpan={columns.length} className={EMPTY_CELL_CLASS}>
								<div className="flex justify-center">
									<Skeleton className="h-3.5 w-40" />
								</div>
							</TableCell>
						</TableRow>
					) : loading ? (
						Array.from({ length: loadingRows }, (_, rowIndex) => (
							<TableRow
								key={`loading-${rowIndex.toString()}`}
								aria-hidden="true"
								className="hover:bg-transparent"
							>
								{table.getVisibleLeafColumns().map((column, columnIndex) => (
									<TableCell key={column.id}>
										{column.columnDef.meta?.skeleton !== undefined ? (
											column.columnDef.meta.skeleton
										) : (
											<div className="flex h-5 items-center">
												<Skeleton
													className={cn(
														"h-3.5",
														SKELETON_WIDTHS[
															(rowIndex + columnIndex) % SKELETON_WIDTHS.length
														],
													)}
												/>
											</div>
										)}
									</TableCell>
								))}
							</TableRow>
						))
					) : rows.length === 0 ? (
						<TableRow>
							<TableCell colSpan={columns.length} className={EMPTY_CELL_CLASS}>
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
									ref={
										virtualizeRows ? rowVirtualizer.measureElement : undefined
									}
								>
									{row.getVisibleCells().map((cell) => (
										<TableCell key={cell.id}>
											{flexRender(
												cell.column.columnDef.cell,
												cell.getContext(),
											)}
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
		</>
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
