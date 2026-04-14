import { useQuery } from "@tanstack/react-query";
import { useTRPC } from "@/api/trpc";

export function useTags() {
	const trpc = useTRPC();
	return useQuery(trpc.tag.list.queryOptions());
}
