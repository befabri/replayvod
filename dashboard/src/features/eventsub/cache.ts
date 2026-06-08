import type { useTRPC } from "@/api/trpc";
import { defineCaches } from "@/lib/query";

// EventSub caches refreshed by config/snapshot/subscription mutations; scalar
// (invalidate-only), so each mutation just names the subset it touches.
export function eventsubCaches(trpc: ReturnType<typeof useTRPC>) {
	return defineCaches({
		config: { path: trpc.eventsub.config, shape: "scalar" },
		listSubscriptions: {
			path: trpc.eventsub.listSubscriptions,
			shape: "scalar",
		},
		listSnapshots: { path: trpc.eventsub.listSnapshots, shape: "scalar" },
		latestSnapshot: { path: trpc.eventsub.latestSnapshot, shape: "scalar" },
	});
}
