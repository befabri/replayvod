import { useMemo, useState, useSyncExternalStore } from "react";

// FullscreenOrientation is the value handed to Vidstack's
// fullscreenOrientation prop: lock the screen to landscape when the player
// goes fullscreen, or leave the screen alone.
export type FullscreenOrientation = "landscape" | "none";

// Browsers only honour screen.orientation.lock() on handhelds, and in
// practice that means Chromium on Android. Desktop Chromium exposes lock()
// and unlock() but rejects every call with NotSupportedError; Firefox did the
// same on desktop, never completed a lock on Android, and dropped both
// methods in version 144; Safari has never implemented lock(). Vidstack only
// checks that the methods exist, so with its default of "landscape" every
// fullscreen entry on a desktop browser attempted a lock, failed, and logged
// the failure.
//
// A touch-primary pointer that cannot hover is the standard signal for the
// handheld class where the lock succeeds. Chromium's mobile client hint
// covers phones that currently have a mouse attached, which report a fine,
// hoverable pointer while the lock still works.
const HANDHELD_QUERY = "(pointer: coarse) and (hover: none)";

type HandheldQuery = {
	matches: boolean;
	addEventListener(type: "change", listener: () => void): void;
	removeEventListener(type: "change", listener: () => void): void;
};

// OrientationHost is the slice of Window the store reads, typed structurally
// so tests can pass plain objects and so the checks do not depend on which
// lib.dom revision declares lock() or the user-agent client hints.
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

// createFullscreenOrientationStore builds the external store the hook reads.
// The lock API and the client hint cannot change while the page lives, so
// they are checked once; only the pointer class is watched.
export function createFullscreenOrientationStore(
	host: OrientationHost | undefined,
): FullscreenOrientationStore {
	if (!host || !hasScreenOrientationLock(host)) return unlockedStore;
	if (hasMobileClientHint(host)) {
		return { ...unlockedStore, getSnapshot: () => "landscape" };
	}
	if (typeof host.matchMedia !== "function") return unlockedStore;
	// One MediaQueryList for the store's lifetime: the pointer class can
	// change at runtime (a tablet docked to a keyboard and mouse, a
	// convertible flipping into tablet mode), and reading `matches` off the
	// same list keeps the snapshot a property read.
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

// useFullscreenOrientation returns the lock type for the current device and
// re-renders when the device's pointer class changes.
//
// Pass hold=true while the player is fullscreen. Vidstack reads the prop on
// each fullscreen change and only sends the unlock request if the value is
// still a lock type, so a value that flipped mid-fullscreen would leave its
// internal locked flag set and block every later lock. Holding the value
// until fullscreen exits keeps each lock paired with its unlock.
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
