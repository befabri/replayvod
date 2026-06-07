// @vitest-environment jsdom

import {
	act,
	cleanup,
	fireEvent,
	render,
	screen,
} from "@testing-library/react";
import type { ComponentProps, ReactNode } from "react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { VideoResponse } from "@/api/generated/trpc";

const useVideoSnapshotsMock = vi.hoisted(() =>
	vi.fn(() => ({ data: [] as string[] })),
);

vi.mock("react-i18next", () => ({
	useTranslation: () => ({
		t: (key: string) => key,
	}),
}));

vi.mock("@tanstack/react-router", async () => {
	const React = await vi.importActual<typeof import("react")>("react");
	type LinkProps = ComponentProps<"a"> & {
		children?: ReactNode;
		params?: unknown;
		search?: unknown;
		to?: unknown;
	};
	return {
		Link: ({
			children,
			params: _params,
			search: _search,
			to: _to,
			...props
		}: LinkProps) =>
			React.createElement("a", { href: "#", ...props }, children),
	};
});

vi.mock("@/features/videos", () => ({
	channelLabel: (video: VideoResponse) =>
		video.broadcaster_name || video.broadcaster_login || video.broadcaster_id,
	useSetWatchLater: () => ({
		isPending: false,
		mutate: vi.fn(),
	}),
	useVideoSnapshots: useVideoSnapshotsMock,
}));

vi.mock("./StreamHistoryButton", async () => {
	const React = await vi.importActual<typeof import("react")>("react");
	return {
		StreamHistoryButton: () =>
			React.createElement("button", { type: "button" }, "history"),
	};
});

import { VideoCard } from "./VideoCard";

const STORED_PREVIEW_BASE = "/api/v1/thumbnails/streamer-2026-01-01-snap00.jpg";

let intersectionCallback: IntersectionObserverCallback | null = null;

class MockIntersectionObserver implements IntersectionObserver {
	readonly root = null;
	readonly rootMargin: string;
	readonly scrollMargin = "";
	readonly thresholds = [];

	constructor(
		callback: IntersectionObserverCallback,
		options?: IntersectionObserverInit,
	) {
		intersectionCallback = callback;
		this.rootMargin = options?.rootMargin ?? "";
	}

	disconnect = vi.fn();
	observe = vi.fn();
	takeRecords = vi.fn(() => []);
	unobserve = vi.fn();
}

function setIntersecting(isIntersecting: boolean) {
	if (!intersectionCallback) {
		throw new Error("IntersectionObserver callback was not registered");
	}
	act(() => {
		intersectionCallback?.(
			[{ isIntersecting } as IntersectionObserverEntry],
			{} as IntersectionObserver,
		);
	});
}

function storedPreviewImg(container: HTMLElement): HTMLImageElement | null {
	return container.querySelector(`img[src^="${STORED_PREVIEW_BASE}"]`);
}

function video(overrides: Partial<VideoResponse> = {}): VideoResponse {
	return {
		id: 1,
		job_id: "job-1",
		filename: "streamer-2026-01-01",
		display_name: "Streamer",
		title: "Live stream",
		status: "RUNNING",
		completion_kind: "complete",
		truncated: false,
		quality: "1080p60",
		is_audio_only: false,
		broadcaster_id: "123",
		broadcaster_login: "streamer",
		broadcaster_name: "Streamer",
		profile_image_url: "",
		viewer_count: 0,
		language: "en",
		duration_seconds: 0,
		size_bytes: 0,
		start_download_at: "2026-01-01T12:00:00Z",
		...overrides,
	};
}

beforeEach(() => {
	intersectionCallback = null;
	vi.stubGlobal("IntersectionObserver", MockIntersectionObserver);
});

afterEach(() => {
	cleanup();
	vi.useRealTimers();
	vi.unstubAllGlobals();
	useVideoSnapshotsMock.mockClear();
});

describe("VideoCard stored preview thumbnail", () => {
	it("shows watch later on running videos", () => {
		render(<VideoCard video={video()} canManage={false} />);

		expect(screen.getByLabelText("videos.watch_later.add")).toBeTruthy();
	});

	it("does not mount stored preview fallback images while the card is off-screen", () => {
		const { container } = render(
			<VideoCard video={video()} canManage={false} />,
		);

		expect(storedPreviewImg(container)).toBeNull();

		setIntersecting(true);

		expect(storedPreviewImg(container)?.getAttribute("src")).toBe(
			STORED_PREVIEW_BASE,
		);
	});

	it("stops retrying a missing stored preview after three cache-busted attempts", () => {
		vi.useFakeTimers();
		const { container } = render(
			<VideoCard video={video()} canManage={false} />,
		);
		setIntersecting(true);

		let img = storedPreviewImg(container);
		expect(img?.getAttribute("src")).toBe(STORED_PREVIEW_BASE);

		for (let retry = 1; retry <= 3; retry += 1) {
			fireEvent.error(img as HTMLImageElement);
			expect(storedPreviewImg(container)).toBeNull();

			act(() => {
				vi.advanceTimersByTime(5000);
			});

			img = storedPreviewImg(container);
			expect(img?.getAttribute("src")).toBe(
				`${STORED_PREVIEW_BASE}?rv=${retry}`,
			);
		}

		fireEvent.error(img as HTMLImageElement);
		expect(storedPreviewImg(container)).toBeNull();

		act(() => {
			vi.advanceTimersByTime(20000);
		});

		expect(storedPreviewImg(container)).toBeNull();
	});

	it("pauses a pending stored preview retry while the card is off-screen", () => {
		vi.useFakeTimers();
		const { container } = render(
			<VideoCard video={video()} canManage={false} />,
		);
		setIntersecting(true);

		const img = storedPreviewImg(container);
		expect(img?.getAttribute("src")).toBe(STORED_PREVIEW_BASE);

		fireEvent.error(img as HTMLImageElement);
		setIntersecting(false);

		act(() => {
			vi.advanceTimersByTime(5000);
		});

		expect(storedPreviewImg(container)).toBeNull();

		setIntersecting(true);
		act(() => {
			vi.advanceTimersByTime(4999);
		});
		expect(storedPreviewImg(container)).toBeNull();

		act(() => {
			vi.advanceTimersByTime(1);
		});
		expect(storedPreviewImg(container)?.getAttribute("src")).toBe(
			`${STORED_PREVIEW_BASE}?rv=1`,
		);
	});
});
