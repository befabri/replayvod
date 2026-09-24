import { useWindowVirtualizer } from "@tanstack/react-virtual";
import type * as React from "react";
import {
	useCallback,
	useEffect,
	useLayoutEffect,
	useMemo,
	useReducer,
	useRef,
	useState,
} from "react";
import { cn } from "@/lib/utils";
import { LoadingState } from "./loading-state";

export function VirtualGrid<TItem>({
	items,
	getItemKey,
	renderItem,
	minItemWidth,
	estimateRowHeight,
	gap = 16,
	overscan = 4,
	className,
	rowClassName,
}: {
	items: TItem[];
	getItemKey: (item: TItem, index: number) => React.Key;
	renderItem: (item: TItem, index: number) => React.ReactNode;
	minItemWidth: number;
	estimateRowHeight: number;
	gap?: number;
	overscan?: number;
	className?: string;
	rowClassName?: string;
}) {
	const rootRef = useRef<HTMLDivElement | null>(null);
	const [width, setWidth] = useState(0);
	const [scrollMargin, setScrollMargin] = useState(0);

	const updateScrollMargin = useCallback(() => {
		const node = rootRef.current;
		if (!node) return;
		const next = node.getBoundingClientRect().top + window.scrollY;
		setScrollMargin((prev) => (Math.abs(prev - next) < 1 ? prev : next));
	}, []);

	useLayoutEffect(() => {
		const node = rootRef.current;
		if (!node) return;

		const updateWidth = () => {
			const next = node.getBoundingClientRect().width;
			setWidth((prev) => (Math.abs(prev - next) < 1 ? prev : next));
		};

		updateWidth();
		if (typeof ResizeObserver === "undefined") {
			window.addEventListener("resize", updateWidth);
			return () => window.removeEventListener("resize", updateWidth);
		}

		const observer = new ResizeObserver(updateWidth);
		observer.observe(node);
		return () => observer.disconnect();
	}, []);

	useLayoutEffect(updateScrollMargin);

	useEffect(() => {
		window.addEventListener("resize", updateScrollMargin);
		return () => window.removeEventListener("resize", updateScrollMargin);
	}, [updateScrollMargin]);

	const columnCount = useMemo(() => {
		if (width <= 0) return 1;
		return Math.max(1, Math.floor((width + gap) / (minItemWidth + gap)));
	}, [gap, minItemWidth, width]);

	const rowCount = Math.ceil(items.length / columnCount);
	const getRowKey = useCallback(
		(rowIndex: number) => {
			const itemIndex = rowIndex * columnCount;
			const item = items[itemIndex];
			return item
				? `${columnCount}:${String(getItemKey(item, itemIndex))}`
				: `${columnCount}:${rowIndex}`;
		},
		[columnCount, getItemKey, items],
	);

	const rowVirtualizer = useWindowVirtualizer<HTMLDivElement>({
		count: rowCount,
		estimateSize: () => estimateRowHeight,
		gap,
		getItemKey: getRowKey,
		overscan,
		scrollMargin,
	});

	// biome-ignore lint/correctness/useExhaustiveDependencies: regrouping rows invalidates cached row heights.
	useEffect(() => {
		rowVirtualizer.measure();
	}, [columnCount, items.length, rowVirtualizer]);

	const virtualRows = rowVirtualizer.getVirtualItems();
	const totalSize = rowVirtualizer.getTotalSize();
	const [, rerender] = useReducer((renders: number) => renders + 1, 0);
	useLayoutEffect(() => {
		const rows = rootRef.current?.querySelectorAll<HTMLElement>("[data-index]");
		for (const row of rows ?? []) {
			rowVirtualizer.resizeItem(Number(row.dataset.index), row.offsetHeight);
		}
		if (rowVirtualizer.getTotalSize() !== totalSize) rerender();
	});

	return (
		<div ref={rootRef} className={cn("relative w-full", className)}>
			<div className="relative w-full" style={{ height: `${totalSize}px` }}>
				{virtualRows.map((virtualRow) => {
					const startIndex = virtualRow.index * columnCount;
					const rowItems = items.slice(startIndex, startIndex + columnCount);

					return (
						<div
							key={virtualRow.key}
							data-index={virtualRow.index}
							ref={rowVirtualizer.measureElement}
							className={cn("absolute top-0 left-0 grid w-full", rowClassName)}
							style={{
								columnGap: `${gap}px`,
								gridTemplateColumns: `repeat(${columnCount}, minmax(0, 1fr))`,
								transform: `translateY(${virtualRow.start - scrollMargin}px)`,
							}}
						>
							{rowItems.map((item, itemOffset) => {
								const itemIndex = startIndex + itemOffset;
								return (
									<div key={getItemKey(item, itemIndex)} className="min-w-0">
										{renderItem(item, itemIndex)}
									</div>
								);
							})}
						</div>
					);
				})}
			</div>
		</div>
	);
}

export function VirtualGridSkeleton({
	count,
	renderItem,
	minItemWidth,
	gap = 16,
	className,
}: {
	count: number;
	renderItem: (index: number) => React.ReactNode;
	minItemWidth: number;
	gap?: number;
	className?: string;
}) {
	return (
		<LoadingState
			className={cn("grid w-full", className)}
			style={{
				gap: `${gap}px`,
				gridTemplateColumns: `repeat(auto-fill, minmax(min(${minItemWidth}px, 100%), 1fr))`,
			}}
		>
			{Array.from({ length: count }, (_, index) => (
				// biome-ignore lint/suspicious/noArrayIndexKey: skeleton slots are positional and never reorder.
				<div key={index} className="min-w-0">
					{renderItem(index)}
				</div>
			))}
		</LoadingState>
	);
}
