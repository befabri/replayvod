import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query"
import { useTRPC } from "@/api/trpc"

export function useUsers() {
	const trpc = useTRPC()
	return useQuery(trpc.system.listUsers.queryOptions())
}

export function useUpdateUserRole() {
	const trpc = useTRPC()
	const queryClient = useQueryClient()
	return useMutation(
		trpc.system.updateUserRole.mutationOptions({
			onSuccess: () => {
				queryClient.invalidateQueries({
					queryKey: trpc.system.listUsers.queryKey(),
				})
			},
		}),
	)
}
