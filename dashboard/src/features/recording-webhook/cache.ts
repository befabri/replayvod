import type { useTRPC } from "@/api/trpc";
import { defineCaches } from "@/lib/query";

// Owner-facing webhook caches; scalar (invalidate-only), each mutation names
// the subset it touches.
export function recordingWebhookCaches(trpc: ReturnType<typeof useTRPC>) {
	return defineCaches({
		config: { path: trpc.recordingWebhook.config, shape: "scalar" },
		deliveries: { path: trpc.recordingWebhook.deliveries, shape: "scalar" },
	});
}
