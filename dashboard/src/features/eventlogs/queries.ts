import { useQuery } from "@tanstack/react-query"
import { useTRPC } from "@/api/trpc"

export function useEventLogs(params: {
	limit: number
	offset: number
	domain?: string
	severity?: string
}) {
	const trpc = useTRPC()
	return useQuery(
		trpc.system.eventLogs.queryOptions({
			limit: params.limit,
			offset: params.offset,
			domain: params.domain ?? "",
			severity: params.severity ?? "",
		}),
	)
}
