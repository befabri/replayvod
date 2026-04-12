import { useQuery } from "@tanstack/react-query"
import { useTRPC } from "@/api/trpc"

export function useFetchLogs(limit: number, offset: number, fetchType?: string) {
	const trpc = useTRPC()
	return useQuery(
		trpc.system.fetchLogs.queryOptions({
			limit,
			offset,
			fetch_type: fetchType ?? "",
		}),
	)
}
