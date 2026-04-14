import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import type { ScheduleListResponse } from "@/api/generated/trpc";
import { useTRPC } from "@/api/trpc";

// Flip `is_disabled` for a schedule in a cached list response.
// Used by the toggle mutation's optimistic update to make the UI
// reflect the new state immediately, so downstream consumers (e.g.
// EditForm's captured `is_disabled` on dialog open) see the fresh
// value without waiting for the server round-trip.
function flipDisabledInList(
	old: ScheduleListResponse | undefined,
	id: number,
): ScheduleListResponse | undefined {
	if (!old?.data) return old;
	return {
		...old,
		data: old.data.map((s) =>
			s.id === id ? { ...s, is_disabled: !s.is_disabled } : s,
		),
	};
}

export function useSchedules(limit = 50, offset = 0) {
	const trpc = useTRPC();
	return useQuery(trpc.schedule.list.queryOptions({ limit, offset }));
}

export function useMineSchedules(limit = 50, offset = 0) {
	const trpc = useTRPC();
	return useQuery(trpc.schedule.mine.queryOptions({ limit, offset }));
}

export function useCreateSchedule() {
	const trpc = useTRPC();
	const queryClient = useQueryClient();
	return useMutation(
		trpc.schedule.create.mutationOptions({
			onSuccess: () => {
				queryClient.invalidateQueries({
					queryKey: trpc.schedule.list.queryKey(),
				});
				queryClient.invalidateQueries({
					queryKey: trpc.schedule.mine.queryKey(),
				});
			},
		}),
	);
}

export function useUpdateSchedule() {
	const trpc = useTRPC();
	const queryClient = useQueryClient();
	return useMutation(
		trpc.schedule.update.mutationOptions({
			onSuccess: () => {
				queryClient.invalidateQueries({
					queryKey: trpc.schedule.list.queryKey(),
				});
				queryClient.invalidateQueries({
					queryKey: trpc.schedule.mine.queryKey(),
				});
			},
		}),
	);
}

export function useToggleSchedule() {
	const trpc = useTRPC();
	const queryClient = useQueryClient();
	return useMutation(
		trpc.schedule.toggle.mutationOptions({
			// Optimistic: flip `is_disabled` in both cached lists before
			// the server responds. Consumers (ScheduleRow badges, EditForm
			// captured state) see the change immediately; if the mutation
			// fails, onError rolls back. Without this, a fast user opening
			// Edit right after clicking Pause captures the pre-toggle value
			// and a subsequent save stomps the just-applied toggle.
			onMutate: async ({ id }) => {
				const listKey = trpc.schedule.list.queryKey();
				const mineKey = trpc.schedule.mine.queryKey();
				await queryClient.cancelQueries({ queryKey: listKey });
				await queryClient.cancelQueries({ queryKey: mineKey });
				const prevList =
					queryClient.getQueryData<ScheduleListResponse>(listKey);
				const prevMine =
					queryClient.getQueryData<ScheduleListResponse>(mineKey);
				queryClient.setQueryData(listKey, (old) =>
					flipDisabledInList(old as ScheduleListResponse, id),
				);
				queryClient.setQueryData(mineKey, (old) =>
					flipDisabledInList(old as ScheduleListResponse, id),
				);
				return { prevList, prevMine };
			},
			onError: (_err, _vars, ctx) => {
				if (ctx?.prevList) {
					queryClient.setQueryData(trpc.schedule.list.queryKey(), ctx.prevList);
				}
				if (ctx?.prevMine) {
					queryClient.setQueryData(trpc.schedule.mine.queryKey(), ctx.prevMine);
				}
			},
			onSettled: () => {
				queryClient.invalidateQueries({
					queryKey: trpc.schedule.list.queryKey(),
				});
				queryClient.invalidateQueries({
					queryKey: trpc.schedule.mine.queryKey(),
				});
			},
		}),
	);
}

export function useDeleteSchedule() {
	const trpc = useTRPC();
	const queryClient = useQueryClient();
	return useMutation(
		trpc.schedule.delete.mutationOptions({
			onSuccess: () => {
				queryClient.invalidateQueries({
					queryKey: trpc.schedule.list.queryKey(),
				});
				queryClient.invalidateQueries({
					queryKey: trpc.schedule.mine.queryKey(),
				});
			},
		}),
	);
}
