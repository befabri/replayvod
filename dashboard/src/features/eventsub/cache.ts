import type { useTRPC } from "@/api/trpc";
import { defineCaches } from "@/lib/query";

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
