import { useInfiniteQuery, useQuery } from "@tanstack/react-query";
import type { CategoryPageResponse } from "@/api/generated/trpc";
import { useTRPC } from "@/api/trpc";

export type CategorySort =
	| "name_asc"
	| "latest_video_desc"
	| "video_count_desc";

export function useCategories() {
	const trpc = useTRPC();
	return useQuery(trpc.category.list.queryOptions());
}

export function useCategoriesWithVideos() {
	const trpc = useTRPC();
	return useQuery(trpc.category.listWithVideos.queryOptions());
}

export function useInfiniteCategoriesWithVideos(sort: CategorySort) {
	const trpc = useTRPC();
	return useInfiniteQuery(
		trpc.category.listPage.infiniteQueryOptions(
			{ limit: 60, sort },
			{
				getNextPageParam: (lastPage: CategoryPageResponse) =>
					lastPage.next_cursor ?? undefined,
			},
		),
	);
}

export function useCategory(id: string) {
	const trpc = useTRPC();
	return useQuery(
		trpc.category.getById.queryOptions({ id }, { enabled: !!id }),
	);
}

export function useCategoryDetail(id: string) {
	const trpc = useTRPC();
	return useQuery(
		trpc.category.getDetail.queryOptions(
			{ id },
			{ enabled: !!id, staleTime: 30_000 },
		),
	);
}

export function useCategorySearch(
	query: string,
	limit = 50,
	options?: { enabled?: boolean },
) {
	const trpc = useTRPC();
	return useQuery(
		trpc.category.search.queryOptions(
			{ query, limit },
			{ enabled: options?.enabled ?? true },
		),
	);
}

export function useCategorySearchWithVideos(
	query: string,
	limit = 50,
	options?: { enabled?: boolean },
) {
	const trpc = useTRPC();
	return useQuery(
		trpc.category.searchWithVideos.queryOptions(
			{ query, limit },
			{ enabled: options?.enabled ?? true },
		),
	);
}
