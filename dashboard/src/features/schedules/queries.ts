import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query"
import { useTRPC } from "@/api/trpc"

export function useSchedules(limit = 50, offset = 0) {
	const trpc = useTRPC()
	return useQuery(trpc.schedule.list.queryOptions({ limit, offset }))
}

export function useMineSchedules(limit = 50, offset = 0) {
	const trpc = useTRPC()
	return useQuery(trpc.schedule.mine.queryOptions({ limit, offset }))
}

export function useCreateSchedule() {
	const trpc = useTRPC()
	const queryClient = useQueryClient()
	return useMutation(
		trpc.schedule.create.mutationOptions({
			onSuccess: () => {
				queryClient.invalidateQueries({ queryKey: trpc.schedule.list.queryKey() })
				queryClient.invalidateQueries({ queryKey: trpc.schedule.mine.queryKey() })
			},
		}),
	)
}

export function useUpdateSchedule() {
	const trpc = useTRPC()
	const queryClient = useQueryClient()
	return useMutation(
		trpc.schedule.update.mutationOptions({
			onSuccess: () => {
				queryClient.invalidateQueries({ queryKey: trpc.schedule.list.queryKey() })
				queryClient.invalidateQueries({ queryKey: trpc.schedule.mine.queryKey() })
			},
		}),
	)
}

export function useToggleSchedule() {
	const trpc = useTRPC()
	const queryClient = useQueryClient()
	return useMutation(
		trpc.schedule.toggle.mutationOptions({
			onSuccess: () => {
				queryClient.invalidateQueries({ queryKey: trpc.schedule.list.queryKey() })
				queryClient.invalidateQueries({ queryKey: trpc.schedule.mine.queryKey() })
			},
		}),
	)
}

export function useDeleteSchedule() {
	const trpc = useTRPC()
	const queryClient = useQueryClient()
	return useMutation(
		trpc.schedule.delete.mutationOptions({
			onSuccess: () => {
				queryClient.invalidateQueries({ queryKey: trpc.schedule.list.queryKey() })
				queryClient.invalidateQueries({ queryKey: trpc.schedule.mine.queryKey() })
			},
		}),
	)
}
