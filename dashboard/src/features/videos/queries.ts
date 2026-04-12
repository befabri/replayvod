import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query"
import { useTRPC } from "@/api/trpc"

export function useVideos(limit = 50, offset = 0, status?: string) {
	const trpc = useTRPC()
	return useQuery(
		trpc.video.list.queryOptions({ limit, offset, status: status ?? "" }),
	)
}

export function useVideo(id: number) {
	const trpc = useTRPC()
	return useQuery(
		trpc.video.getById.queryOptions({ id }, { enabled: id > 0 }),
	)
}

export function useVideosByBroadcaster(broadcasterId: string, limit = 50, offset = 0) {
	const trpc = useTRPC()
	return useQuery(
		trpc.video.byBroadcaster.queryOptions(
			{ broadcaster_id: broadcasterId, limit, offset },
			{ enabled: !!broadcasterId },
		),
	)
}

export function useVideosByCategory(categoryId: string, limit = 50, offset = 0) {
	const trpc = useTRPC()
	return useQuery(
		trpc.video.byCategory.queryOptions(
			{ category_id: categoryId, limit, offset },
			{ enabled: !!categoryId },
		),
	)
}

export function useStatistics() {
	const trpc = useTRPC()
	return useQuery(trpc.video.statistics.queryOptions())
}

export function useTriggerDownload() {
	const trpc = useTRPC()
	const queryClient = useQueryClient()
	return useMutation(
		trpc.video.triggerDownload.mutationOptions({
			onSuccess: () => {
				queryClient.invalidateQueries({ queryKey: trpc.video.list.queryKey() })
				queryClient.invalidateQueries({
					queryKey: trpc.video.statistics.queryKey(),
				})
			},
		}),
	)
}

export function useCancelDownload() {
	const trpc = useTRPC()
	const queryClient = useQueryClient()
	return useMutation(
		trpc.video.cancel.mutationOptions({
			onSuccess: () => {
				queryClient.invalidateQueries({ queryKey: trpc.video.list.queryKey() })
			},
		}),
	)
}
