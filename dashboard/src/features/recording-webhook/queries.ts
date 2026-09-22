import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useTRPC } from "@/api/trpc";
import { invalidateCaches } from "@/lib/query";
import { recordingWebhookCaches } from "./cache";

export function useRecordingWebhookConfig() {
	const trpc = useTRPC();
	return useQuery(trpc.recordingWebhook.config.queryOptions());
}

export function useRecordingWebhookDeliveries() {
	const trpc = useTRPC();
	return useQuery({
		...trpc.recordingWebhook.deliveries.queryOptions(),
		refetchInterval: 15_000,
	});
}

export function useUpdateRecordingWebhookConfig() {
	const trpc = useTRPC();
	const queryClient = useQueryClient();
	const caches = recordingWebhookCaches(trpc);
	return useMutation(
		trpc.recordingWebhook.updateConfig.mutationOptions({
			onSuccess: () => invalidateCaches(queryClient, caches, ["config"]),
		}),
	);
}

export function useRetryRecordingWebhookDelivery() {
	const trpc = useTRPC();
	const queryClient = useQueryClient();
	const caches = recordingWebhookCaches(trpc);
	return useMutation(
		trpc.recordingWebhook.retryDelivery.mutationOptions({
			onSuccess: () => invalidateCaches(queryClient, caches, ["deliveries"]),
		}),
	);
}

export function useRegenerateRecordingWebhookSecret() {
	const trpc = useTRPC();
	const queryClient = useQueryClient();
	const caches = recordingWebhookCaches(trpc);
	return useMutation(
		trpc.recordingWebhook.regenerateSecret.mutationOptions({
			onSuccess: () => invalidateCaches(queryClient, caches, ["config"]),
		}),
	);
}

export function useTestRecordingWebhookDelivery() {
	const trpc = useTRPC();
	const queryClient = useQueryClient();
	const caches = recordingWebhookCaches(trpc);
	return useMutation(
		trpc.recordingWebhook.testDelivery.mutationOptions({
			onSuccess: () =>
				invalidateCaches(queryClient, caches, ["deliveries", "config"]),
		}),
	);
}
