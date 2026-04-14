import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useTRPC } from "@/api/trpc";

export function useChannels() {
	const trpc = useTRPC();
	return useQuery(trpc.channel.list.queryOptions());
}

export function useFollowedChannels() {
	const trpc = useTRPC();
	return useQuery(trpc.channel.listFollowed.queryOptions());
}

export function useChannel(broadcasterId: string) {
	const trpc = useTRPC();
	return useQuery(
		trpc.channel.getById.queryOptions(
			{ broadcaster_id: broadcasterId },
			{ enabled: !!broadcasterId },
		),
	);
}

export function useChannelSearch(query: string, limit = 10) {
	const trpc = useTRPC();
	// Empty query returns all channels up to limit (per backend contract) —
	// that's what powers the combobox's "show all" initial state.
	return useQuery(trpc.channel.search.queryOptions({ query, limit }));
}

export function useSyncChannel() {
	const trpc = useTRPC();
	const queryClient = useQueryClient();
	return useMutation(
		trpc.channel.syncFromTwitch.mutationOptions({
			onSuccess: () => {
				queryClient.invalidateQueries({
					queryKey: trpc.channel.list.queryKey(),
				});
			},
		}),
	);
}
