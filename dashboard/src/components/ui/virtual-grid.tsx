import { useWindowVirtualizer } from "@tanstack/react-virtual";
import type * as React from "react";
import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { cn } from "@/lib/utils";

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

	useEffect(() => {
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

	useEffect(updateScrollMargin);

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

	return (
		<div ref={rootRef} className={cn("relative w-full", className)}>
			<div
				className="relative w-full"
				style={{ height: `${rowVirtualizer.getTotalSize()}px` }}
			>
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
