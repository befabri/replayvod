import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useSubscription } from "@trpc/tanstack-react-query";
import { useTRPC } from "@/api/trpc";

export function useTasks() {
	const trpc = useTRPC();
	return useQuery(trpc.task.list.queryOptions());
}

export function useToggleTask() {
	const trpc = useTRPC();
	const queryClient = useQueryClient();
	return useMutation(
		trpc.task.toggle.mutationOptions({
			onSuccess: () => {
				queryClient.invalidateQueries({ queryKey: trpc.task.list.queryKey() });
			},
		}),
	);
}

export function useRunTaskNow() {
	const trpc = useTRPC();
	const queryClient = useQueryClient();
	return useMutation(
		trpc.task.runNow.mutationOptions({
			onSuccess: () => {
				queryClient.invalidateQueries({ queryKey: trpc.task.list.queryKey() });
			},
		}),
	);
}

// useLiveTaskStatus attaches to the task.status SSE feed and
// invalidates the task list on every transition. The list reload
// (one cheap query) is the simpler path than optimistic patching
// here — a task row has ~10 fields all of which move on each
// transition, so there's nothing to save by patching by hand.
export function useLiveTaskStatus() {
	const trpc = useTRPC();
	const queryClient = useQueryClient();
	useSubscription({
		...trpc.task.status.subscriptionOptions(),
		onData: () => {
			queryClient.invalidateQueries({ queryKey: trpc.task.list.queryKey() });
		},
	});
}
