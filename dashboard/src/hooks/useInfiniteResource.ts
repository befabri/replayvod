import { useMemo, useRef } from "react";
import { useInfiniteScrollSentinel } from "./useInfiniteScrollSentinel";

type InfinitePagesQuery<Page> = {
	data: { pages: Page[] } | undefined;
	hasNextPage: boolean;
	error: unknown;
	isFetchingNextPage: boolean;
	fetchNextPage: () => Promise<unknown>;
};

type UseInfiniteResourceOptions<Page, Item> = {
	getItems: (page: Page) => Item[];
	rootMargin?: string;
	shouldLoadMore?: (ctx: {
		items: Item[];
		query: InfinitePagesQuery<Page>;
	}) => boolean;
};

export function useInfiniteResource<Page, Item>(
	query: InfinitePagesQuery<Page>,
	options: UseInfiniteResourceOptions<Page, Item>,
) {
	const { getItems, rootMargin, shouldLoadMore: shouldLoadMoreFn } = options;

	const getItemsRef = useRef(getItems);
	getItemsRef.current = getItems;
	const pages = query.data?.pages;
	const items = useMemo(
		() => pages?.flatMap((page) => getItemsRef.current(page)) ?? [],
		[pages],
	);

	const pageCount = pages?.length ?? 0;
	const hasScrolledThroughPages = pageCount > 1;
	const shouldLoadMore = shouldLoadMoreFn
		? shouldLoadMoreFn({ items, query })
		: items.length > 0 && query.hasNextPage && !query.error;

	const loadMoreRef = useInfiniteScrollSentinel({
		enabled: shouldLoadMore,
		isLoadingMore: query.isFetchingNextPage,
		onLoadMore: () => query.fetchNextPage(),
		rootMargin,
	});

	const showEnd =
		hasScrolledThroughPages &&
		!query.hasNextPage &&
		!query.isFetchingNextPage &&
		items.length > 0;

	return {
		items,
		pageCount,
		hasScrolledThroughPages,
		shouldLoadMore,
		loadMoreRef,
		showEnd,
	};
}
