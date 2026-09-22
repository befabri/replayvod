import { useMemo, useState, useSyncExternalStore } from "react";

export type FullscreenOrientation = "landscape" | "none";

const HANDHELD_QUERY = "(pointer: coarse) and (hover: none)";

type HandheldQuery = {
	matches: boolean;
	addEventListener(type: "change", listener: () => void): void;
	removeEventListener(type: "change", listener: () => void): void;
};

export type OrientationHost = {
	matchMedia?: (query: string) => HandheldQuery;
	navigator?: object;
	screen?: { orientation?: object | null };
};

export type FullscreenOrientationStore = {
	subscribe: (onChange: () => void) => () => void;
	getSnapshot: () => FullscreenOrientation;
};

const unlockedStore: FullscreenOrientationStore = {
	subscribe: () => () => {},
	getSnapshot: () => "none",
};

function hasScreenOrientationLock(host: OrientationHost): boolean {
	const orientation = host.screen?.orientation;
	if (!orientation) return false;
	return (
		"lock" in orientation &&
		typeof orientation.lock === "function" &&
		"unlock" in orientation &&
		typeof orientation.unlock === "function"
	);
}

function hasMobileClientHint(host: OrientationHost): boolean {
	const nav = host.navigator;
	if (!nav || !("userAgentData" in nav)) return false;
	const hints = nav.userAgentData;
	return (
		typeof hints === "object" &&
		hints !== null &&
		"mobile" in hints &&
		hints.mobile === true
	);
}

export function createFullscreenOrientationStore(
	host: OrientationHost | undefined,
): FullscreenOrientationStore {
	if (!host || !hasScreenOrientationLock(host)) return unlockedStore;
	if (hasMobileClientHint(host)) {
		return { ...unlockedStore, getSnapshot: () => "landscape" };
	}
	if (typeof host.matchMedia !== "function") return unlockedStore;
	const query = host.matchMedia(HANDHELD_QUERY);
	return {
		subscribe: (onChange) => {
			query.addEventListener("change", onChange);
			return () => query.removeEventListener("change", onChange);
		},
		getSnapshot: () => (query.matches ? "landscape" : "none"),
	};
}

function getServerSnapshot(): FullscreenOrientation {
	return "none";
}

export function useFullscreenOrientation(hold = false): FullscreenOrientation {
	const store = useMemo(
		() =>
			createFullscreenOrientationStore(
				typeof window === "undefined" ? undefined : window,
			),
		[],
	);
	const live = useSyncExternalStore(
		store.subscribe,
		store.getSnapshot,
		getServerSnapshot,
	);
	const [held, setHeld] = useState(live);
	if (!hold && held !== live) setHeld(live);
	return held;
}
