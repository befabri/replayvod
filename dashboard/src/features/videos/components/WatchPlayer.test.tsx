// @vitest-environment jsdom

import {
	cleanup,
	fireEvent,
	render,
	screen,
	waitFor,
} from "@testing-library/react";
import type { ReactNode } from "react";
import { afterEach, describe, expect, it, vi } from "vitest";
import type { RecordingPlaylist } from "@/features/videos/playback";
import { WatchPlayer } from "./WatchPlayer";

type MockPlayer = {
	currentTime: number;
	paused: boolean;
	playbackRate: number;
	play: ReturnType<typeof vi.fn<() => Promise<void>>>;
};

const vidstackMock = vi.hoisted(() => ({
	player: {
		currentTime: 0,
		paused: false,
		playbackRate: 1,
		play: vi.fn(() => Promise.resolve()),
	} as MockPlayer,
}));

vi.mock("react-i18next", () => ({
	useTranslation: () => ({
		t: (key: string, vars?: Record<string, string | number>) => {
			if (!vars) return key;
			return `${key}:${Object.entries(vars)
				.map(([name, value]) => `${name}=${value}`)
				.join(",")}`;
		},
	}),
}));

vi.mock("@vidstack/react", async () => {
	const React = await vi.importActual<typeof import("react")>("react");
	function sourceSrc(src: unknown): string {
		if (typeof src === "string") return src;
		if (src && typeof src === "object" && "src" in src) {
			const value = src.src;
			return typeof value === "string" ? value : "";
		}
		return "";
	}
	type MockMediaPlayerProps = {
		children?: ReactNode;
		src?: unknown;
		onCanPlay?: () => void;
		onEnded?: () => void;
		onError?: () => void;
		onTimeUpdate?: (detail: { currentTime: number }) => void;
		onKeyDown?: React.KeyboardEventHandler<HTMLElement>;
		onPause?: () => void;
		onPlay?: () => void;
	};
	const MediaPlayer = React.forwardRef<MockPlayer, MockMediaPlayerProps>(
		(
			{
				children,
				src,
				onCanPlay,
				onEnded,
				onError,
				onTimeUpdate,
				onKeyDown,
				onPause,
				onPlay,
			},
			ref,
		) => {
			React.useImperativeHandle(ref, () => vidstackMock.player);
			return React.createElement(
				"section",
				{
					"data-testid": "media-player",
					"data-src": sourceSrc(src),
					onKeyDown,
					tabIndex: 0,
				},
				React.createElement(
					"button",
					{ type: "button", onClick: onEnded },
					"ended",
				),
				React.createElement(
					"button",
					{ type: "button", onClick: onCanPlay },
					"canplay",
				),
				React.createElement(
					"button",
					{ type: "button", onClick: onError },
					"error",
				),
				React.createElement(
					"button",
					{ type: "button", onClick: onPlay },
					"play",
				),
				React.createElement(
					"button",
					{ type: "button", onClick: onPause },
					"pause",
				),
				React.createElement(
					"button",
					{
						type: "button",
						onClick: () =>
							onTimeUpdate?.({ currentTime: vidstackMock.player.currentTime }),
					},
					"timeupdate",
				),
				children,
			);
		},
	);
	return {
		MediaPlayer,
		MediaProvider: ({ children }: { children?: ReactNode }) =>
			React.createElement("div", { "data-testid": "media-provider" }, children),
		Track: () => React.createElement("track"),
	};
});

vi.mock("@vidstack/react/player/layouts/default", async () => {
	const React = await vi.importActual<typeof import("react")>("react");
	return {
		defaultLayoutIcons: {},
		DefaultVideoLayout: ({ slots }: { slots?: { timeSlider?: ReactNode } }) =>
			React.createElement(
				"div",
				{ "data-testid": "layout" },
				slots?.timeSlider,
			),
	};
});

afterEach(() => {
	cleanup();
	vidstackMock.player.currentTime = 0;
	vidstackMock.player.paused = false;
	vidstackMock.player.playbackRate = 1;
	vidstackMock.player.play.mockClear();
});

describe("WatchPlayer multipart boundaries", () => {
	it("resumes on the next part and restores playback rate after natural playback advances", async () => {
		const playlist = multipartPlaylist();
		vidstackMock.player.paused = false;
		vidstackMock.player.playbackRate = 1.75;

		render(<WatchPlayer playlist={playlist} />);
		expect(screen.getByTestId("media-player").getAttribute("data-src")).toBe(
			"/part-1.mp4",
		);

		fireEvent.click(screen.getByRole("button", { name: "ended" }));

		expect(screen.getByTestId("media-player").getAttribute("data-src")).toBe(
			"/part-2.mp4",
		);
		fireEvent.click(screen.getByRole("button", { name: "canplay" }));

		await waitFor(() => {
			expect(vidstackMock.player.currentTime).toBe(0);
			expect(vidstackMock.player.playbackRate).toBe(1.75);
			expect(vidstackMock.player.play).toHaveBeenCalledTimes(1);
		});
	});

	it("keeps a cross-part marker seek paused and restores playback rate", () => {
		const playlist = multipartPlaylist();
		vidstackMock.player.paused = true;
		vidstackMock.player.playbackRate = 1.5;

		render(<WatchPlayer playlist={playlist} />);
		fireEvent.click(screen.getByRole("button", { name: /Second title/ }));

		expect(screen.getByTestId("media-player").getAttribute("data-src")).toBe(
			"/part-2.mp4",
		);
		fireEvent.click(screen.getByRole("button", { name: "canplay" }));

		expect(vidstackMock.player.currentTime).toBe(10);
		expect(vidstackMock.player.playbackRate).toBe(1.5);
		expect(vidstackMock.player.play).not.toHaveBeenCalled();
	});

	it("seeks within a hovered part period instead of only at the split marker", () => {
		const playlist = multipartPlaylist();
		vidstackMock.player.paused = true;

		render(<WatchPlayer playlist={playlist} />);
		const partPeriod = screen.getByRole("button", {
			name: /watch\.part_segment:part=2/,
		});
		mockRect(requiredParent(partPeriod), { left: 0, width: 120 });

		fireEvent.click(partPeriod, { clientX: 90 });

		expect(screen.getByTestId("media-player").getAttribute("data-src")).toBe(
			"/part-2.mp4",
		);
		fireEvent.click(screen.getByRole("button", { name: "canplay" }));

		expect(vidstackMock.player.currentTime).toBe(30);
		expect(vidstackMock.player.play).not.toHaveBeenCalled();
	});

	it("drags within a part period using the full recording timeline", () => {
		const playlist = multipartPlaylist();
		vidstackMock.player.paused = true;

		render(<WatchPlayer playlist={playlist} />);
		const partPeriod = screen.getByRole("button", {
			name: /watch\.part_segment:part=2/,
		});
		mockRect(requiredParent(partPeriod), { left: 0, width: 120 });

		fireEvent.pointerDown(partPeriod, { button: 0, clientX: 60, pointerId: 1 });
		fireEvent.pointerMove(partPeriod, { clientX: 105, pointerId: 1 });
		fireEvent.pointerUp(partPeriod, { clientX: 105, pointerId: 1 });

		expect(screen.getByTestId("media-player").getAttribute("data-src")).toBe(
			"/part-2.mp4",
		);
		fireEvent.click(screen.getByRole("button", { name: "canplay" }));

		expect(vidstackMock.player.currentTime).toBe(45);
		expect(vidstackMock.player.play).not.toHaveBeenCalled();
	});

	it("drags across a part boundary using the global cursor position", () => {
		const playlist = multipartPlaylist();
		vidstackMock.player.paused = true;

		render(<WatchPlayer playlist={playlist} />);
		const partPeriod = screen.getByRole("button", {
			name: /watch\.part_segment:part=1/,
		});
		mockRect(requiredParent(partPeriod), { left: 0, width: 120 });

		fireEvent.pointerDown(partPeriod, { button: 0, clientX: 30, pointerId: 1 });
		fireEvent.pointerMove(partPeriod, { clientX: 90, pointerId: 1 });
		fireEvent.pointerUp(partPeriod, { clientX: 90, pointerId: 1 });

		expect(screen.getByTestId("media-player").getAttribute("data-src")).toBe(
			"/part-2.mp4",
		);
		fireEvent.click(screen.getByRole("button", { name: "canplay" }));

		expect(vidstackMock.player.currentTime).toBe(30);
		expect(vidstackMock.player.play).not.toHaveBeenCalled();
	});

	it("uses one provided continuous source when the playlist has one", () => {
		const playlist = continuousPlaylist();

		render(<WatchPlayer playlist={playlist} />);

		expect(screen.getByTestId("media-player").getAttribute("data-src")).toBe(
			"/api/v1/videos/65/playback/stream",
		);
		fireEvent.click(screen.getByRole("button", { name: "ended" }));
		expect(screen.getByTestId("media-player").getAttribute("data-src")).toBe(
			"/api/v1/videos/65/playback/stream",
		);
	});

	it("crosses a part boundary on the continuous source without remounting playback state", async () => {
		const playlist = continuousPlaylist();
		vidstackMock.player.paused = false;
		vidstackMock.player.playbackRate = 1.75;

		render(<WatchPlayer playlist={playlist} />);
		vidstackMock.player.currentTime = 70;
		fireEvent.click(screen.getByRole("button", { name: "timeupdate" }));

		await waitFor(() => {
			expect(screen.getByTestId("media-player").getAttribute("data-src")).toBe(
				"/api/v1/videos/65/playback/stream",
			);
			expect(screen.getByText(/watch\.part_status:current=2/)).toBeTruthy();
			expect(vidstackMock.player.playbackRate).toBe(1.75);
			expect(vidstackMock.player.play).not.toHaveBeenCalled();
		});
	});

	it("seeks markers on the continuous source by global media time", () => {
		const playlist = continuousPlaylist();
		vidstackMock.player.paused = true;

		render(<WatchPlayer playlist={playlist} />);
		// Media has loaded (canplay) before the user clicks a marker.
		fireEvent.click(screen.getByRole("button", { name: "canplay" }));
		fireEvent.click(screen.getByRole("button", { name: /Second title/ }));

		expect(screen.getByTestId("media-player").getAttribute("data-src")).toBe(
			"/api/v1/videos/65/playback/stream",
		);
		expect(vidstackMock.player.currentTime).toBe(70);
		expect(vidstackMock.player.play).not.toHaveBeenCalled();
	});

	it("seeks to the continuous artifact duration when it exceeds summed parts", () => {
		const playlist = {
			...continuousPlaylist(),
			totalDurationSeconds: 122,
		};
		vidstackMock.player.paused = true;

		render(<WatchPlayer playlist={playlist} />);
		fireEvent.click(screen.getByRole("button", { name: "canplay" }));
		fireEvent.keyDown(screen.getByTestId("media-player"), { key: "End" });

		expect(vidstackMock.player.currentTime).toBe(122);
		expect(vidstackMock.player.play).not.toHaveBeenCalled();
	});

	it("maps player time onto the recording timeline when the muxed file drifts", () => {
		const playlist: RecordingPlaylist = {
			...continuousPlaylist(),
			continuousSource: {
				src: "/api/v1/videos/65/playback/stream",
				mimeType: "video/mp4",
				// Muxed file probes to half the 120s recording timeline.
				durationSeconds: 60,
			},
		};
		vidstackMock.player.paused = true;

		render(<WatchPlayer playlist={playlist} />);
		fireEvent.click(screen.getByRole("button", { name: "canplay" }));

		// Seeking the recording-offset-70 marker maps onto the 60s muxed clock:
		// 70 * (60 / 120) = 35.
		fireEvent.click(screen.getByRole("button", { name: /Second title/ }));
		expect(vidstackMock.player.currentTime).toBe(35);

		// A player timeupdate at 30 maps back to recording offset 60 (30 * 120/60),
		// which lands on part 2.
		vidstackMock.player.currentTime = 30;
		fireEvent.click(screen.getByRole("button", { name: "timeupdate" }));
		expect(screen.getByText(/watch\.part_status:current=2/)).toBeTruthy();
	});

	it("applies a deep-link initialOffsetSeconds once the continuous source is ready", () => {
		const playlist = continuousPlaylist();
		vidstackMock.player.paused = true;

		render(<WatchPlayer playlist={playlist} initialOffsetSeconds={70} />);

		// Cold load: the media hasn't fired canplay, so the seek must be deferred,
		// NOT assigned into the void (the bug this guards).
		expect(vidstackMock.player.currentTime).toBe(0);

		fireEvent.click(screen.getByRole("button", { name: "canplay" }));

		expect(vidstackMock.player.currentTime).toBe(70);
		// Deep-link seek must not auto-resume a paused player.
		expect(vidstackMock.player.play).not.toHaveBeenCalled();
	});

	it("falls back to part sequencing when a continuous source errors", () => {
		const playlist = continuousPlaylist();
		vidstackMock.player.currentTime = 70;
		vidstackMock.player.paused = false;
		vidstackMock.player.playbackRate = 1.5;

		render(<WatchPlayer playlist={playlist} />);
		fireEvent.click(screen.getByRole("button", { name: "play" }));
		fireEvent.click(screen.getByRole("button", { name: "error" }));

		expect(screen.getByTestId("media-player").getAttribute("data-src")).toBe(
			"/part-2.mp4",
		);
		fireEvent.click(screen.getByRole("button", { name: "canplay" }));

		expect(vidstackMock.player.currentTime).toBe(10);
		expect(vidstackMock.player.playbackRate).toBe(1.5);
		expect(vidstackMock.player.play).toHaveBeenCalledTimes(1);
	});

	it("upgrades to the single-file source mid-watch when the artifact becomes ready, resuming in place", () => {
		const { rerender } = render(<WatchPlayer playlist={multipartPlaylist()} />);

		// Starts in the part sequencer: the artifact is built lazily and isn't
		// ready yet.
		expect(screen.getByTestId("media-player").getAttribute("data-src")).toBe(
			"/part-1.mp4",
		);

		// 30s into part 1, playing.
		vidstackMock.player.currentTime = 30;
		vidstackMock.player.paused = false;
		fireEvent.click(screen.getByRole("button", { name: "play" }));
		fireEvent.click(screen.getByRole("button", { name: "timeupdate" }));

		// The lazily-built artifact lands (the getById poll refreshes the playlist).
		rerender(<WatchPlayer playlist={continuousPlaylist()} />);

		// The player swaps to the single continuous file...
		expect(screen.getByTestId("media-player").getAttribute("data-src")).toBe(
			"/api/v1/videos/65/playback/stream",
		);

		// ...and resumes at the carried-over position once it can play, instead of
		// restarting from 0 (the freshly-loaded source reports time 0).
		vidstackMock.player.currentTime = 0;
		fireEvent.click(screen.getByRole("button", { name: "canplay" }));
		expect(vidstackMock.player.currentTime).toBe(30);
		expect(vidstackMock.player.play).toHaveBeenCalledTimes(1);
	});
});

function multipartPlaylist(): RecordingPlaylist {
	return {
		videoId: 65,
		title: "Recording",
		totalDurationSeconds: 120,
		continuousSource: null,
		parts: [
			{
				partIndex: 1,
				position: 0,
				src: "/part-1.mp4",
				mimeType: "video/mp4",
				durationSeconds: 60,
				sizeBytes: 100,
				startSeconds: 0,
				endSeconds: 60,
				label: "Part 1",
			},
			{
				partIndex: 2,
				position: 1,
				src: "/part-2.mp4",
				mimeType: "video/mp4",
				durationSeconds: 60,
				sizeBytes: 200,
				startSeconds: 60,
				endSeconds: 120,
				label: "Part 2",
			},
		],
		markers: [
			{
				key: "second-title",
				offsetSeconds: 70,
				label: "Second title",
				kind: "title",
				changes: ["title"],
				event: {
					occurred_at: "2026-01-01T00:01:10Z",
					media_offset_seconds: 70,
					title: { id: 2, name: "Second title" },
				},
			},
		],
	};
}

function continuousPlaylist(): RecordingPlaylist {
	return {
		...multipartPlaylist(),
		continuousSource: {
			src: "/api/v1/videos/65/playback/stream",
			mimeType: "video/mp4",
			durationSeconds: null,
		},
	};
}

function requiredParent(element: HTMLElement): HTMLElement {
	const parent = element.parentElement;
	if (!parent) throw new Error("expected element parent");
	return parent;
}

function mockRect(
	element: Element,
	{ left, width }: { left: number; width: number },
) {
	element.getBoundingClientRect = () =>
		({
			left,
			width,
			right: left + width,
			top: 0,
			bottom: 20,
			height: 20,
			x: left,
			y: 0,
			toJSON: () => ({}),
		}) as DOMRect;
}
