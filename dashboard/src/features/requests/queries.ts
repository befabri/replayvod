import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query"
import { useTRPC } from "@/api/trpc"

export function useMyRequests(limit = 50, offset = 0) {
	const trpc = useTRPC()
	return useQuery(trpc.videorequest.mine.queryOptions({ limit, offset }))
}

export function useRequestVideo() {
	const trpc = useTRPC()
	const queryClient = useQueryClient()
	return useMutation(
		trpc.videorequest.request.mutationOptions({
			onSuccess: () => {
				queryClient.invalidateQueries({
					queryKey: trpc.videorequest.mine.queryKey(),
				})
			},
		}),
	)
}
