import type { useTRPC } from "@/api/trpc";
import { defineCaches } from "@/lib/query";

export function recordingWebhookCaches(trpc: ReturnType<typeof useTRPC>) {
	return defineCaches({
		config: { path: trpc.recordingWebhook.config, shape: "scalar" },
		deliveries: { path: trpc.recordingWebhook.deliveries, shape: "scalar" },
	});
}
