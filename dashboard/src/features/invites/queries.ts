import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useTRPC } from "@/api/trpc";

export function useInvites() {
	const trpc = useTRPC();
	return useQuery(trpc.system.listInvites.queryOptions());
}

export function useCreateInvite() {
	const trpc = useTRPC();
	const queryClient = useQueryClient();
	return useMutation(
		trpc.system.createInvite.mutationOptions({
			onSuccess: () => {
				queryClient.invalidateQueries({
					queryKey: trpc.system.listInvites.pathKey(),
				});
			},
		}),
	);
}

export function useRevokeInvite() {
	const trpc = useTRPC();
	const queryClient = useQueryClient();
	return useMutation(
		trpc.system.revokeInvite.mutationOptions({
			onSuccess: () => {
				queryClient.invalidateQueries({
					queryKey: trpc.system.listInvites.pathKey(),
				});
			},
		}),
	);
}
