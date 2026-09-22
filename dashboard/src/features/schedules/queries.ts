import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import type {
	PauseStateResponse,
	ScheduleResponse,
	ScheduleToggleInput,
	SetPausedInput,
} from "@/api/generated/trpc";
import { useTRPC } from "@/api/trpc";
import { invalidateCaches, optimisticWrite, patchEntity } from "@/lib/query";
import {
	scheduleCaches,
	schedulePauseCaches,
	scheduleToggleDisabledPatch,
} from "./cache";

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
	const caches = scheduleCaches(trpc);
	return useMutation(
		trpc.schedule.create.mutationOptions({
			onSuccess: () => invalidateCaches(queryClient, caches),
		}),
	);
}

export function useUpdateSchedule() {
	const trpc = useTRPC();
	const queryClient = useQueryClient();
	const caches = scheduleCaches(trpc);
	return useMutation(
		trpc.schedule.update.mutationOptions({
			onSuccess: () => invalidateCaches(queryClient, caches),
		}),
	);
}

export function useToggleSchedule() {
	const trpc = useTRPC();
	const queryClient = useQueryClient();
	const caches = scheduleCaches(trpc);
	return useMutation(
		trpc.schedule.toggle.mutationOptions(
			optimisticWrite<ScheduleResponse, ScheduleToggleInput>(
				queryClient,
				caches,
				{
					apply: (qc, { id }) =>
						patchEntity(qc, caches, scheduleToggleDisabledPatch(id)),
				},
			),
		),
	);
}

export function useSchedulesPaused() {
	const trpc = useTRPC();
	return useQuery(trpc.schedule.pauseState.queryOptions(undefined));
}

export function useSetSchedulesPaused() {
	const trpc = useTRPC();
	const queryClient = useQueryClient();
	const caches = schedulePauseCaches(trpc);
	return useMutation(
		trpc.schedule.setPaused.mutationOptions(
			optimisticWrite<PauseStateResponse, SetPausedInput>(queryClient, caches, {
				apply: (qc, { paused }) =>
					qc.setQueriesData<PauseStateResponse>(
						{ queryKey: caches.pauseState.pathKey },
						() => ({ paused }),
					),
			}),
		),
	);
}

export function useDeleteSchedule() {
	const trpc = useTRPC();
	const queryClient = useQueryClient();
	const caches = scheduleCaches(trpc);
	return useMutation(
		trpc.schedule.delete.mutationOptions({
			onSuccess: () => invalidateCaches(queryClient, caches),
		}),
	);
}
