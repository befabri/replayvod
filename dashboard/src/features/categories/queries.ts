import { useQuery } from "@tanstack/react-query";
import { useTRPC } from "@/api/trpc";

export function useCategories() {
	const trpc = useTRPC();
	return useQuery(trpc.category.list.queryOptions());
}

export function useCategory(id: string) {
	const trpc = useTRPC();
	return useQuery(
		trpc.category.getById.queryOptions({ id }, { enabled: !!id }),
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
