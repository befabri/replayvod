import {
	useInfiniteQuery,
	useMutation,
	useQuery,
	useQueryClient,
} from "@tanstack/react-query";
import { useSubscription } from "@trpc/tanstack-react-query";
import type { ChannelVODsResponse } from "@/api/generated/trpc";
import { useTRPC } from "@/api/trpc";
import { videoCaches } from "@/features/videos/cache";
import { invalidateCaches, resyncQuery } from "@/lib/query";
import { withSessionProbe } from "@/stores/auth";

export const CHANNEL_VODS_PAGE_SIZE = 30;

export function useChannelVods(channel: string | null) {
	const trpc = useTRPC();
	return useInfiniteQuery(
		trpc.archive.listChannelVods.infiniteQueryOptions(
			{ channel: channel ?? "", limit: CHANNEL_VODS_PAGE_SIZE },
			{
				enabled: channel != null && channel !== "",
				getNextPageParam: (lastPage: ChannelVODsResponse) =>
					lastPage.next_cursor ?? undefined,
				staleTime: 60_000,
				retry: false,
			},
		),
	);
}

export function useLiveArchiveQueue() {
	const trpc = useTRPC();
	const queryClient = useQueryClient();
	useSubscription({
		...trpc.archive.queueLive.subscriptionOptions(),
		onStarted: () => resyncQuery(queryClient, trpc.archive.queue.pathKey()),
		onData: () => resyncQuery(queryClient, trpc.archive.queue.pathKey()),
		onError: withSessionProbe(),
	});
}

export function useArchiveQueue() {
	const trpc = useTRPC();
	return useQuery(
		trpc.archive.queue.queryOptions(undefined, {
			refetchOnWindowFocus: true,
		}),
	);
}

function useInvalidateArchive() {
	const trpc = useTRPC();
	const queryClient = useQueryClient();
	const caches = videoCaches(trpc);
	return () => {
		void queryClient.invalidateQueries({
			queryKey: trpc.archive.queue.pathKey(),
		});
		void queryClient.invalidateQueries({
			queryKey: trpc.archive.listChannelVods.pathKey(),
		});
		invalidateCaches(queryClient, caches);
	};
}

export function useEnqueueArchive() {
	const trpc = useTRPC();
	const invalidate = useInvalidateArchive();
	return useMutation(
		trpc.archive.enqueue.mutationOptions({ onSuccess: invalidate }),
	);
}

export function useDequeueArchive() {
	const trpc = useTRPC();
	const invalidate = useInvalidateArchive();
	return useMutation(
		trpc.archive.dequeue.mutationOptions({ onSuccess: invalidate }),
	);
}

export function useRetryArchive() {
	const trpc = useTRPC();
	const invalidate = useInvalidateArchive();
	return useMutation(
		trpc.archive.retry.mutationOptions({ onSuccess: invalidate }),
	);
}

export function useCancelArchiveRetry() {
	const trpc = useTRPC();
	const invalidate = useInvalidateArchive();
	return useMutation(
		trpc.archive.cancelRetry.mutationOptions({ onSuccess: invalidate }),
	);
}
