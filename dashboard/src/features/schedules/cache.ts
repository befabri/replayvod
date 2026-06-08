import type { ScheduleResponse } from "@/api/generated/trpc";
import type { useTRPC } from "@/api/trpc";
import { defineCaches, type EntityPatch } from "@/lib/query";

// The two schedule list caches. Both are { data: ScheduleResponse[] } envelopes
// keyed by { limit, offset } input, so patches must go through pathKey (a bare
// no-input queryKey would never match the real input-keyed entry).
export function scheduleCaches(trpc: ReturnType<typeof useTRPC>) {
	return defineCaches({
		list: { path: trpc.schedule.list, shape: "wrapped" },
		mine: { path: trpc.schedule.mine, shape: "wrapped" },
	});
}

// The global auto-download pause flag. Scalar (snapshot/invalidate only); the
// optimistic write sets it directly rather than through a row patch.
export function schedulePauseCaches(trpc: ReturnType<typeof useTRPC>) {
	return defineCaches({
		pauseState: { path: trpc.schedule.pauseState, shape: "scalar" },
	});
}

// Flip is_disabled for one schedule wherever it appears in the cached lists.
export function scheduleToggleDisabledPatch(
	id: number,
): EntityPatch<ScheduleResponse> {
	return {
		match: (schedule) => schedule.id === id,
		update: (schedule) => ({ ...schedule, is_disabled: !schedule.is_disabled }),
	};
}
