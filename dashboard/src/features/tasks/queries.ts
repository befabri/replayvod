import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query"
import { useTRPC } from "@/api/trpc"

export function useTasks() {
	const trpc = useTRPC()
	return useQuery(trpc.task.list.queryOptions())
}

export function useToggleTask() {
	const trpc = useTRPC()
	const queryClient = useQueryClient()
	return useMutation(
		trpc.task.toggle.mutationOptions({
			onSuccess: () => {
				queryClient.invalidateQueries({ queryKey: trpc.task.list.queryKey() })
			},
		}),
	)
}

export function useRunTaskNow() {
	const trpc = useTRPC()
	const queryClient = useQueryClient()
	return useMutation(
		trpc.task.runNow.mutationOptions({
			onSuccess: () => {
				queryClient.invalidateQueries({ queryKey: trpc.task.list.queryKey() })
			},
		}),
	)
}
