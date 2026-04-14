import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useTRPC } from "@/api/trpc";

export function useWhitelist() {
	const trpc = useTRPC();
	return useQuery(trpc.system.listWhitelist.queryOptions());
}

export function useAddWhitelist() {
	const trpc = useTRPC();
	const queryClient = useQueryClient();
	return useMutation(
		trpc.system.addWhitelist.mutationOptions({
			onSuccess: () => {
				queryClient.invalidateQueries({
					queryKey: trpc.system.listWhitelist.queryKey(),
				});
			},
		}),
	);
}

export function useRemoveWhitelist() {
	const trpc = useTRPC();
	const queryClient = useQueryClient();
	return useMutation(
		trpc.system.removeWhitelist.mutationOptions({
			onSuccess: () => {
				queryClient.invalidateQueries({
					queryKey: trpc.system.listWhitelist.queryKey(),
				});
			},
		}),
	);
}
