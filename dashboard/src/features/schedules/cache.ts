import type { ScheduleResponse } from "@/api/generated/trpc";
import type { useTRPC } from "@/api/trpc";
import { defineCaches, type EntityPatch } from "@/lib/query";

export function scheduleCaches(trpc: ReturnType<typeof useTRPC>) {
	return defineCaches({
		list: { path: trpc.schedule.list, shape: "wrapped" },
		mine: { path: trpc.schedule.mine, shape: "wrapped" },
	});
}

export function schedulePauseCaches(trpc: ReturnType<typeof useTRPC>) {
	return defineCaches({
		pauseState: { path: trpc.schedule.pauseState, shape: "scalar" },
	});
}

export function scheduleToggleDisabledPatch(
	id: number,
): EntityPatch<ScheduleResponse> {
	return {
		match: (schedule) => schedule.id === id,
		update: (schedule) => ({ ...schedule, is_disabled: !schedule.is_disabled }),
	};
}
