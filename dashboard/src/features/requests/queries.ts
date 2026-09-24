import {
	useInfiniteQuery,
	useMutation,
	useQueryClient,
} from "@tanstack/react-query";
import type { RequestPageResponse } from "@/api/generated/trpc";
import { useTRPC } from "@/api/trpc";

export function useMyScheduleRequests({
	enabled = true,
}: {
	enabled?: boolean;
} = {}) {
	const trpc = useTRPC();
	return useInfiniteQuery({
		...trpc.schedule.myRequests.infiniteQueryOptions(
			{ limit: 50 },
			{
				getNextPageParam: (page: RequestPageResponse) =>
					page.next_cursor ?? undefined,
			},
		),
		select: (data) => data.pages.flatMap((page) => page.items),
		enabled,
	});
}

export function useAllScheduleRequests({
	enabled = true,
}: {
	enabled?: boolean;
} = {}) {
	const trpc = useTRPC();
	return useInfiniteQuery({
		...trpc.schedule.requests.infiniteQueryOptions(
			{ limit: 50 },
			{
				getNextPageParam: (page: RequestPageResponse) =>
					page.next_cursor ?? undefined,
			},
		),
		select: (data) => data.pages.flatMap((page) => page.items),
		enabled,
	});
}

function useInvalidateRequests() {
	const trpc = useTRPC();
	const queryClient = useQueryClient();
	return () => {
		queryClient.invalidateQueries({
			queryKey: trpc.schedule.myRequests.pathKey(),
		});
		queryClient.invalidateQueries({
			queryKey: trpc.schedule.requests.pathKey(),
		});
	};
}

export function useCreateScheduleRequest() {
	const trpc = useTRPC();
	const invalidate = useInvalidateRequests();
	return useMutation(
		trpc.schedule.createRequest.mutationOptions({ onSuccess: invalidate }),
	);
}

export function useCancelScheduleRequest() {
	const trpc = useTRPC();
	const invalidate = useInvalidateRequests();
	return useMutation(
		trpc.schedule.cancelRequest.mutationOptions({ onSuccess: invalidate }),
	);
}

export function useRejectScheduleRequest() {
	const trpc = useTRPC();
	const invalidate = useInvalidateRequests();
	return useMutation(
		trpc.schedule.rejectRequest.mutationOptions({ onSuccess: invalidate }),
	);
}

export function useApproveScheduleRequest() {
	const trpc = useTRPC();
	const queryClient = useQueryClient();
	const invalidate = useInvalidateRequests();
	return useMutation(
		trpc.schedule.approveRequest.mutationOptions({
			onSuccess: () => {
				invalidate();
				queryClient.invalidateQueries({
					queryKey: trpc.schedule.list.pathKey(),
				});
				queryClient.invalidateQueries({
					queryKey: trpc.schedule.mine.pathKey(),
				});
			},
		}),
	);
}
