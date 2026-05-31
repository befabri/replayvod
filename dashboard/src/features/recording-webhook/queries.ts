import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useTRPC } from "@/api/trpc";

// useRecordingWebhookConfig loads the owner-managed outbound webhook config.
// Owner-only on the server; the System route already gates the page.
export function useRecordingWebhookConfig() {
	const trpc = useTRPC();
	return useQuery(trpc.recordingWebhook.config.queryOptions());
}

// useRecordingWebhookDeliveries loads the durable delivery log (newest first)
// so the dashboard can show whether deliveries are actually landing.
export function useRecordingWebhookDeliveries() {
	const trpc = useTRPC();
	return useQuery({
		...trpc.recordingWebhook.deliveries.queryOptions(),
		refetchInterval: 15_000,
	});
}

// useUpdateRecordingWebhookConfig saves the config and refreshes the cached
// query so the displayed state reflects the persisted result.
export function useUpdateRecordingWebhookConfig() {
	const trpc = useTRPC();
	const queryClient = useQueryClient();
	return useMutation(
		trpc.recordingWebhook.updateConfig.mutationOptions({
			onSuccess: () => {
				queryClient.invalidateQueries({
					queryKey: trpc.recordingWebhook.config.queryKey(),
				});
			},
		}),
	);
}

export function useRetryRecordingWebhookDelivery() {
	const trpc = useTRPC();
	const queryClient = useQueryClient();
	return useMutation(
		trpc.recordingWebhook.retryDelivery.mutationOptions({
			onSuccess: () => {
				queryClient.invalidateQueries({
					queryKey: trpc.recordingWebhook.deliveries.queryKey(),
				});
			},
		}),
	);
}

// useRegenerateRecordingWebhookSecret rotates the signing secret server-side and
// refreshes the config so the new secret is shown. Separate from the save path:
// rotating a key is a deliberate, standalone action.
export function useRegenerateRecordingWebhookSecret() {
	const trpc = useTRPC();
	const queryClient = useQueryClient();
	return useMutation(
		trpc.recordingWebhook.regenerateSecret.mutationOptions({
			onSuccess: () => {
				queryClient.invalidateQueries({
					queryKey: trpc.recordingWebhook.config.queryKey(),
				});
			},
		}),
	);
}

// useTestRecordingWebhookDelivery fires a one-off signed test delivery. SendTest
// can also seed a missing signing secret, so refresh both the visible delivery
// history and the config card.
export function useTestRecordingWebhookDelivery() {
	const trpc = useTRPC();
	const queryClient = useQueryClient();
	return useMutation(
		trpc.recordingWebhook.testDelivery.mutationOptions({
			onSuccess: () => {
				queryClient.invalidateQueries({
					queryKey: trpc.recordingWebhook.deliveries.queryKey(),
				});
				queryClient.invalidateQueries({
					queryKey: trpc.recordingWebhook.config.queryKey(),
				});
			},
		}),
	);
}
