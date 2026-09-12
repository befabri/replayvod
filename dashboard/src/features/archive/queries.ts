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

// useChannelVods pages through a channel's VODs on Twitch, newest first. The
// lookup burns Helix quota, so it only runs once the user submits a channel.
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

// useLiveArchiveQueue follows archive queue transitions. The server publishes
// every queue transition (enqueue, start, completion, failure, dequeue, retry
// cancelled) on archive.queueLive, and each event refetches the queue, so the
// page follows the pump without polling. Mount it once above the queue.
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

// useArchiveQueue lists what is queued or running and what failed recently.
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

// useRetryArchive queues a new attempt of a failed archive right away.
export function useRetryArchive() {
	const trpc = useTRPC();
	const invalidate = useInvalidateArchive();
	return useMutation(
		trpc.archive.retry.mutationOptions({ onSuccess: invalidate }),
	);
}

// useCancelArchiveRetry drops the automatic retry of a failed archive.
export function useCancelArchiveRetry() {
	const trpc = useTRPC();
	const invalidate = useInvalidateArchive();
	return useMutation(
		trpc.archive.cancelRetry.mutationOptions({ onSuccess: invalidate }),
	);
}
