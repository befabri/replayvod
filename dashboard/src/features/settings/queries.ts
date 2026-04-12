import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query"
import { useTRPC } from "@/api/trpc"

export function useSettings() {
	const trpc = useTRPC()
	return useQuery(trpc.settings.get.queryOptions())
}

export function useUpdateSettings() {
	const trpc = useTRPC()
	const queryClient = useQueryClient()
	return useMutation(
		trpc.settings.update.mutationOptions({
			onSuccess: () => {
				queryClient.invalidateQueries({
					queryKey: trpc.settings.get.queryKey(),
				})
			},
		}),
	)
}
