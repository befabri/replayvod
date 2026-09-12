import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useTRPC } from "@/api/trpc";

export function useFetchLogs(
	limit: number,
	offset: number,
	fetchType?: string,
) {
	const trpc = useTRPC();
	return useQuery(
		trpc.system.fetchLogs.queryOptions({
			limit,
			offset,
			fetch_type: fetchType ?? "",
		}),
	);
}

export function usePlaybackCacheConfig() {
	const trpc = useTRPC();
	return useQuery(trpc.system.playbackCacheConfig.queryOptions());
}

export function useUpdatePlaybackCacheConfig() {
	const trpc = useTRPC();
	const queryClient = useQueryClient();
	return useMutation(
		trpc.system.updatePlaybackCacheConfig.mutationOptions({
			onSuccess: () => {
				queryClient.invalidateQueries({
					queryKey: trpc.system.playbackCacheConfig.pathKey(),
				});
			},
		}),
	);
}

// useTwitchPlaybackStatus reads the owner-only playback connection state.
// Callers gate it on the owner role; a viewer's request would only 403.
export function useTwitchPlaybackStatus() {
	const trpc = useTRPC();
	return useQuery(trpc.twitchPlayback.status.queryOptions());
}
