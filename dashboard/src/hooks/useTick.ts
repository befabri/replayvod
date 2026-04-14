import { useEffect, useState } from "react";

// Shared timers keyed by interval. Each component that calls useTick(ms)
// subscribes to the same timer as other callers with the same interval,
// so we run at most one setInterval per distinct interval app-wide
// regardless of how many consumers mount.
type Entry = {
	count: number;
	tick: number;
	id: ReturnType<typeof setInterval> | null;
	listeners: Set<(tick: number) => void>;
};

const registry = new Map<number, Entry>();

function subscribe(intervalMs: number, listener: (tick: number) => void) {
	let entry = registry.get(intervalMs);
	if (!entry) {
		entry = {
			count: 0,
			tick: 0,
			id: null,
			listeners: new Set(),
		};
		registry.set(intervalMs, entry);
	}
	entry.listeners.add(listener);
	entry.count++;
	if (entry.id === null) {
		entry.id = setInterval(() => {
			const current = registry.get(intervalMs);
			if (!current) return;
			current.tick++;
			for (const l of current.listeners) l(current.tick);
		}, intervalMs);
	}
	return () => {
		const current = registry.get(intervalMs);
		if (!current) return;
		current.listeners.delete(listener);
		current.count--;
		if (current.count === 0) {
			if (current.id !== null) clearInterval(current.id);
			registry.delete(intervalMs);
		}
	};
}

// useTick forces a re-render every `intervalMs`. Multiple consumers with
// the same interval share one setInterval — only one timer fires per
// interval app-wide, and they stay in sync.
export function useTick(intervalMs: number): number {
	const [tick, setTick] = useState(() => registry.get(intervalMs)?.tick ?? 0);
	useEffect(() => subscribe(intervalMs, setTick), [intervalMs]);
	return tick;
}
