// @vitest-environment jsdom

import { act, renderHook } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import {
	createFullscreenOrientationStore,
	useFullscreenOrientation,
} from "./fullscreen-orientation";

const lockApi = {
	lock: () => Promise.resolve(),
	unlock: () => Promise.resolve(),
};

function handheldQuery(matches: boolean) {
	const listeners = new Set<() => void>();
	return {
		matches,
		listeners,
		addEventListener(_type: "change", listener: () => void) {
			listeners.add(listener);
		},
		removeEventListener(_type: "change", listener: () => void) {
			listeners.delete(listener);
		},
		set(next: boolean) {
			this.matches = next;
			for (const listener of listeners) listener();
		},
	};
}

describe("createFullscreenOrientationStore", () => {
	it("locks landscape on a handheld that exposes the orientation API", () => {
		const store = createFullscreenOrientationStore({
			screen: { orientation: lockApi },
			matchMedia: () => handheldQuery(true),
		});
		expect(store.getSnapshot()).toBe("landscape");
	});

	// Desktop Chromium exposes lock() but rejects every call with
	// NotSupportedError, so the API's presence alone must not enable the lock.
	it("stays unlocked on a desktop browser that exposes the API", () => {
		const store = createFullscreenOrientationStore({
			screen: { orientation: lockApi },
			matchMedia: () => handheldQuery(false),
		});
		expect(store.getSnapshot()).toBe("none");
	});

	it("locks landscape on a phone with a mouse attached", () => {
		const store = createFullscreenOrientationStore({
			screen: { orientation: lockApi },
			navigator: { userAgentData: { mobile: true } },
			matchMedia: () => handheldQuery(false),
		});
		expect(store.getSnapshot()).toBe("landscape");
	});

	it("lets the pointer class decide when the client hint is not mobile", () => {
		const desktopHint = { userAgentData: { mobile: false } };
		expect(
			createFullscreenOrientationStore({
				screen: { orientation: lockApi },
				navigator: desktopHint,
				matchMedia: () => handheldQuery(false),
			}).getSnapshot(),
		).toBe("none");
		expect(
			createFullscreenOrientationStore({
				screen: { orientation: lockApi },
				navigator: desktopHint,
				matchMedia: () => handheldQuery(true),
			}).getSnapshot(),
		).toBe("landscape");
	});

	it("stays unlocked when the browser lacks lock or unlock", () => {
		const handheld = () => handheldQuery(true);
		// Safari has screen.orientation without lock().
		expect(
			createFullscreenOrientationStore({
				screen: { orientation: { type: "portrait-primary" } },
				matchMedia: handheld,
			}).getSnapshot(),
		).toBe("none");
		expect(
			createFullscreenOrientationStore({
				screen: { orientation: { lock: lockApi.lock } },
				matchMedia: handheld,
			}).getSnapshot(),
		).toBe("none");
		expect(
			createFullscreenOrientationStore({
				screen: {},
				matchMedia: handheld,
			}).getSnapshot(),
		).toBe("none");
	});

	it("stays unlocked without matchMedia or without a window", () => {
		expect(
			createFullscreenOrientationStore({
				screen: { orientation: lockApi },
			}).getSnapshot(),
		).toBe("none");
		expect(createFullscreenOrientationStore(undefined).getSnapshot()).toBe(
			"none",
		);
	});

	it("reads the pointer class from one media query list", () => {
		const query = handheldQuery(false);
		const matchMedia = vi.fn(() => query);
		const store = createFullscreenOrientationStore({
			screen: { orientation: lockApi },
			matchMedia,
		});
		const onChange = vi.fn();
		const unsubscribe = store.subscribe(onChange);
		expect(store.getSnapshot()).toBe("none");
		query.set(true);
		expect(onChange).toHaveBeenCalledTimes(1);
		expect(store.getSnapshot()).toBe("landscape");
		unsubscribe();
		expect(query.listeners.size).toBe(0);
		expect(matchMedia).toHaveBeenCalledTimes(1);
	});
});

describe("useFullscreenOrientation", () => {
	afterEach(() => {
		vi.unstubAllGlobals();
	});

	function stubHandheld(matches: boolean) {
		const query = handheldQuery(matches);
		vi.stubGlobal(
			"matchMedia",
			vi.fn(() => query),
		);
		vi.stubGlobal("screen", { orientation: lockApi });
		return query;
	}

	it("follows the pointer class as it changes", () => {
		const query = stubHandheld(false);

		const { result, unmount } = renderHook(() => useFullscreenOrientation());
		expect(result.current).toBe("none");

		act(() => query.set(true));
		expect(result.current).toBe("landscape");

		act(() => query.set(false));
		expect(result.current).toBe("none");

		unmount();
		expect(query.listeners.size).toBe(0);
	});

	it("holds the value while fullscreen and releases it afterwards", () => {
		const query = stubHandheld(true);

		const { result, rerender } = renderHook(
			({ hold }) => useFullscreenOrientation(hold),
			{ initialProps: { hold: false } },
		);
		expect(result.current).toBe("landscape");

		rerender({ hold: true });
		act(() => query.set(false));
		expect(result.current).toBe("landscape");

		rerender({ hold: false });
		expect(result.current).toBe("none");
	});

	it("stays unlocked where the lock cannot be honoured", () => {
		// jsdom ships neither matchMedia nor screen.orientation, the same shape
		// as a browser without the lock.
		const { result } = renderHook(() => useFullscreenOrientation());
		expect(result.current).toBe("none");
	});
});
