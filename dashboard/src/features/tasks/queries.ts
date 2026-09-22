import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useSubscription } from "@trpc/tanstack-react-query";
import { useTRPC } from "@/api/trpc";
import { resyncQuery } from "@/lib/query";
import { withSessionProbe } from "@/stores/auth";

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
				queryClient.invalidateQueries({ queryKey: trpc.task.list.pathKey() });
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
				queryClient.invalidateQueries({ queryKey: trpc.task.list.pathKey() });
			},
		}),
	);
}

export function useLiveTaskStatus() {
	const trpc = useTRPC();
	const queryClient = useQueryClient();
	useSubscription({
		...trpc.task.status.subscriptionOptions(),
		onStarted: () => resyncQuery(queryClient, trpc.task.list.pathKey()),
		onData: () => resyncQuery(queryClient, trpc.task.list.pathKey()),
		onError: withSessionProbe(),
	});
}
