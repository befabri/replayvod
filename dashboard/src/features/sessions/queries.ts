import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query"
import { useTRPC } from "@/api/trpc"

export function useSessions() {
	const trpc = useTRPC()
	return useQuery(trpc.auth.sessions.queryOptions())
}

export function useRevokeSession() {
	const trpc = useTRPC()
	const queryClient = useQueryClient()
	return useMutation(
		trpc.auth.revokeSession.mutationOptions({
			onSuccess: () => {
				queryClient.invalidateQueries({
					queryKey: trpc.auth.sessions.queryKey(),
				})
			},
		}),
	)
}
