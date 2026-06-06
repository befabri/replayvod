import { useEffect, useRef } from "react";

type UseInfiniteScrollSentinelOptions = {
	enabled: boolean;
	isLoadingMore: boolean;
	onLoadMore: () => Promise<unknown> | undefined;
	rootMargin?: string;
	threshold?: number;
};

export function useInfiniteScrollSentinel({
	enabled,
	isLoadingMore,
	onLoadMore,
	rootMargin = "400px 0px",
	threshold,
}: UseInfiniteScrollSentinelOptions) {
	const sentinelRef = useRef<HTMLDivElement | null>(null);
	const onLoadMoreRef = useRef(onLoadMore);

	useEffect(() => {
		onLoadMoreRef.current = onLoadMore;
	}, [onLoadMore]);

	useEffect(() => {
		const node = sentinelRef.current;
		if (!enabled || !node || typeof IntersectionObserver === "undefined") {
			return;
		}

		const observer = new IntersectionObserver(
			(entries) => {
				if (!entries[0]?.isIntersecting || isLoadingMore) {
					return;
				}
				void onLoadMoreRef.current();
			},
			{ rootMargin, threshold },
		);
		observer.observe(node);
		return () => observer.disconnect();
	}, [enabled, isLoadingMore, rootMargin, threshold]);

	return sentinelRef;
}
