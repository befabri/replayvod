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
			search,
			to: _to,
			...props
		}: LinkProps) =>
			React.createElement(
				"a",
				{ href: "#", "data-search": JSON.stringify(search), ...props },
				children,
			),
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
		source: "live",
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

describe("VideoCard resume progress", () => {
	function userState(last_position_seconds: number) {
		return {
			watch_later: false,
			last_position_seconds,
			watched_at: "2026-01-01T12:30:00Z",
			updated_at: "2026-01-01T12:30:00Z",
		};
	}

	it("shows how far a partly watched recording got", () => {
		render(
			<VideoCard
				video={video({
					status: "DONE",
					duration_seconds: 3600,
					user_state: userState(900),
				})}
				canManage={false}
			/>,
		);
		const bar = screen.getByTestId("video-card-progress");
		expect(bar.getAttribute("aria-valuenow")).toBe("25");
		expect(bar.getAttribute("aria-label")).toBe("videos.resume_progress");
	});

	it.each([
		["never played", undefined],
		["barely started", 3],
		["played to the end", 3590],
	])("shows no bar when %s", (_, position) => {
		render(
			<VideoCard
				video={video({
					status: "DONE",
					duration_seconds: 3600,
					user_state: position == null ? undefined : userState(position),
				})}
				canManage={false}
			/>,
		);
		expect(screen.queryByTestId("video-card-progress")).toBeNull();
	});

	it("shows no bar on a recording still downloading", () => {
		render(
			<VideoCard
				video={video({ duration_seconds: 3600, user_state: userState(900) })}
				canManage={false}
			/>,
		);
		expect(screen.queryByTestId("video-card-progress")).toBeNull();
	});
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

describe("VideoCard status and playability", () => {
	it("marks a running recording as downloading, hides the play overlay, and leads to the queue", () => {
		render(
			<VideoCard video={video({ status: "RUNNING" })} canManage={false} />,
		);

		expect(screen.getByTestId("video-card-status").textContent).toBe(
			"videos.status.RUNNING",
		);
		expect(screen.queryByTestId("video-card-play")).toBeNull();
		expect(screen.getByLabelText("videos.open_downloads")).toBeTruthy();
		expect(screen.queryByLabelText("videos.watch_recording")).toBeNull();
	});

	it("marks a failed recording and leads to its history entry", () => {
		render(
			<VideoCard
				video={video({ status: "FAILED", truncated: false })}
				canManage={false}
			/>,
		);

		expect(screen.getByTestId("video-card-status").textContent).toBe(
			"videos.status.FAILED",
		);
		expect(screen.queryByTestId("video-card-play")).toBeNull();
		expect(screen.getByLabelText("videos.open_history")).toBeTruthy();
	});

	it("labels a cancelled recording as cancelled rather than failed", () => {
		render(
			<VideoCard
				video={video({ status: "FAILED", completion_kind: "cancelled" })}
				canManage={false}
			/>,
		);

		expect(screen.getByTestId("video-card-status").textContent).toBe(
			"videos.status.CANCELLED",
		);
		expect(
			JSON.parse(
				screen
					.getByLabelText("videos.open_history")
					.getAttribute("data-search") ?? "{}",
			),
		).toEqual({ outcome: "cancelled", media: "any" });
	});

	it("marks an archive and shows the date its stream aired", () => {
		render(
			<VideoCard
				video={video({
					status: "DONE",
					source: "vod",
					twitch_video_id: "77",
					broadcast_at: "2025-12-24T20:00:00Z",
					start_download_at: "2026-01-01T12:00:00Z",
				})}
				canManage={false}
			/>,
		);
		expect(screen.getByTestId("video-card-archive").textContent).toBe(
			"videos.archive_badge",
		);
		const date = screen.getByTestId("video-card-date");
		expect(date.textContent).toBe(
			new Date("2025-12-24T20:00:00Z").toLocaleDateString(),
		);
		expect(date.getAttribute("title")).toBe("videos.streamed_on");
		expect(screen.getByTestId("video-card-archived-on").textContent).toBe(
			"videos.archived_on",
		);
	});

	it("shows the recording date and no archive marker on a live recording", () => {
		render(<VideoCard video={video({ status: "DONE" })} canManage={false} />);
		expect(screen.queryByTestId("video-card-archive")).toBeNull();
		expect(screen.queryByTestId("video-card-archived-on")).toBeNull();
		expect(screen.getByTestId("video-card-date").textContent).toBe(
			new Date("2026-01-01T12:00:00Z").toLocaleDateString(),
		);
	});

	it("keeps the play overlay and no status badge on a finished recording", () => {
		render(<VideoCard video={video({ status: "DONE" })} canManage={false} />);

		expect(screen.getByTestId("video-card-play")).toBeTruthy();
		expect(screen.queryByTestId("video-card-status")).toBeNull();
		expect(screen.getByLabelText("videos.watch_recording")).toBeTruthy();
	});
});
