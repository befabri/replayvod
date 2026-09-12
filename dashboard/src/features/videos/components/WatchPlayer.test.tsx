// @vitest-environment jsdom

import {
	act,
	cleanup,
	fireEvent,
	render,
	screen,
	waitFor,
} from "@testing-library/react";
import {
	type ButtonHTMLAttributes,
	type MouseEvent,
	type ReactNode,
	StrictMode,
} from "react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { formatPlaybackTime } from "@/features/videos/format";
import type { RecordingPlaylist } from "@/features/videos/playback";
import { WatchPlayer } from "./WatchPlayer";

type MockPlayer = {
	canPlay: boolean;
	canSetVolume: boolean;
	currentTime: number;
	ended: boolean;
	muted: boolean;
	paused: boolean;
	playbackRate: number;
	volume: number;
	addEventListener: ReturnType<typeof vi.fn>;
	removeEventListener: ReturnType<typeof vi.fn>;
	play: ReturnType<typeof vi.fn<(trigger?: Event) => Promise<void>>>;
	pause: ReturnType<typeof vi.fn<(trigger?: Event) => Promise<void>>>;
};

const vidstackMock = vi.hoisted(() => {
	const playerListeners = new Map<string, Set<EventListener>>();
	const player = {
		canPlay: false,
		canSetVolume: true,
		currentTime: 0,
		ended: false,
		muted: false,
		paused: false,
		playbackRate: 1,
		volume: 1,
		addEventListener: vi.fn(
			(type: string, listener: EventListenerOrEventListenerObject) => {
				const eventListener =
					typeof listener === "function"
						? listener
						: listener.handleEvent.bind(listener);
				let listeners = playerListeners.get(type);
				if (!listeners) {
					listeners = new Set();
					playerListeners.set(type, listeners);
				}
				listeners.add(eventListener);
			},
		),
		removeEventListener: vi.fn(
			(type: string, listener: EventListenerOrEventListenerObject) => {
				const eventListener =
					typeof listener === "function"
						? listener
						: listener.handleEvent.bind(listener);
				playerListeners.get(type)?.delete(eventListener);
			},
		),
		play: vi.fn(() => Promise.resolve()),
		pause: vi.fn(() => Promise.resolve()),
	} as MockPlayer;

	return {
		dispatchPlayerEvent: (type: string) => {
			const event = new Event(type);
			for (const listener of Array.from(playerListeners.get(type) ?? [])) {
				listener(event);
			}
		},
		player,
		playerListeners,
		remote: null as unknown as {
			changePlaybackRate: ReturnType<typeof vi.fn<(rate: number) => void>>;
			changeVolume: ReturnType<typeof vi.fn<(volume: number) => void>>;
			mute: ReturnType<typeof vi.fn<() => void>>;
			seek: ReturnType<typeof vi.fn<(time: number, trigger?: Event) => void>>;
			seeking: ReturnType<
				typeof vi.fn<(time: number, trigger?: Event) => void>
			>;
			unmute: ReturnType<typeof vi.fn<() => void>>;
		},
	};
});

vidstackMock.remote = {
	changePlaybackRate: vi.fn((rate: number) => {
		vidstackMock.player.playbackRate = rate;
	}),
	changeVolume: vi.fn((volume: number) => {
		vidstackMock.player.volume = volume;
		vidstackMock.player.muted = volume <= 0;
	}),
	mute: vi.fn(() => {
		vidstackMock.player.muted = true;
	}),
	seek: vi.fn((time: number) => {
		vidstackMock.player.currentTime = time;
		vidstackMock.player.ended = false;
	}),
	seeking: vi.fn(),
	unmute: vi.fn(() => {
		vidstackMock.player.muted = false;
	}),
};

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
	function sourceType(src: unknown): string {
		if (src && typeof src === "object" && "type" in src) {
			const value = src.type;
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
		onFullscreenChange?: (active: boolean) => void;
		fullscreenOrientation?: string;
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
				onFullscreenChange,
				fullscreenOrientation,
			},
			ref,
		) => {
			React.useImperativeHandle(ref, () => vidstackMock.player);
			return React.createElement(
				"section",
				{
					"data-testid": "media-player",
					"data-src": sourceSrc(src),
					"data-type": sourceType(src),
					"data-fullscreen-orientation": fullscreenOrientation,
					onKeyDown,
					tabIndex: 0,
				},
				React.createElement(
					"button",
					{
						type: "button",
						onClick: () => {
							vidstackMock.player.ended = true;
							onEnded?.();
						},
					},
					"ended",
				),
				React.createElement(
					"button",
					{
						type: "button",
						onClick: () => {
							vidstackMock.player.canPlay = true;
							onCanPlay?.();
						},
					},
					"canplay",
				),
				React.createElement(
					"button",
					{ type: "button", onClick: onError },
					"error",
				),
				React.createElement(
					"button",
					{
						type: "button",
						onClick: () => {
							vidstackMock.player.paused = false;
							onPlay?.();
						},
					},
					"play",
				),
				React.createElement(
					"button",
					{
						type: "button",
						onClick: () => {
							vidstackMock.player.paused = true;
							onPause?.();
						},
					},
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
				React.createElement(
					"button",
					{ type: "button", onClick: () => onFullscreenChange?.(true) },
					"enter fullscreen",
				),
				React.createElement(
					"button",
					{ type: "button", onClick: () => onFullscreenChange?.(false) },
					"exit fullscreen",
				),
				children,
			);
		},
	);
	return {
		MediaPlayer,
		MediaProvider: ({ children }: { children?: ReactNode }) =>
			React.createElement("div", { "data-testid": "media-provider" }, children),
		PlayButton: ({
			children,
			...props
		}: ButtonHTMLAttributes<HTMLButtonElement> & {
			children?: ReactNode;
		}) =>
			React.createElement(
				"button",
				{
					type: "button",
					...props,
					onClick: (event: MouseEvent<HTMLButtonElement>) => {
						if (vidstackMock.player.paused) {
							vidstackMock.player.paused = false;
							void vidstackMock.player.play();
						} else {
							vidstackMock.player.paused = true;
							void vidstackMock.player.pause();
						}
						props.onClick?.(event);
					},
				},
				children,
			),
		Track: () => React.createElement("track"),
		useMediaRemote: () => vidstackMock.remote,
		useMediaState: (prop: string) =>
			(vidstackMock.player as Record<string, unknown>)[prop],
	};
});

vi.mock("@vidstack/react/player/layouts/default", async () => {
	const React = await vi.importActual<typeof import("react")>("react");
	return {
		defaultLayoutIcons: {},
		DefaultVideoLayout: ({ slots }: { slots?: { timeSlider?: ReactNode } }) =>
			React.createElement(
				"div",
				{ "data-testid": "video-layout" },
				slots?.timeSlider,
			),
	};
});

afterEach(() => {
	cleanup();
	vi.restoreAllMocks();
	vi.unstubAllGlobals();
	vi.useRealTimers();
	vidstackMock.player.canPlay = false;
	vidstackMock.player.canSetVolume = true;
	vidstackMock.player.currentTime = 0;
	vidstackMock.player.ended = false;
	vidstackMock.player.muted = false;
	vidstackMock.player.paused = false;
	vidstackMock.player.playbackRate = 1;
	vidstackMock.player.volume = 1;
	vidstackMock.player.addEventListener.mockClear();
	vidstackMock.player.removeEventListener.mockClear();
	vidstackMock.player.play.mockClear();
	vidstackMock.player.pause.mockClear();
	vidstackMock.playerListeners.clear();
	vidstackMock.remote.changePlaybackRate.mockClear();
	vidstackMock.remote.changeVolume.mockClear();
	vidstackMock.remote.mute.mockClear();
	vidstackMock.remote.seek.mockClear();
	vidstackMock.remote.seeking.mockClear();
	vidstackMock.remote.unmute.mockClear();
});

describe("WatchPlayer multipart boundaries", () => {
	it("emits throttled playback progress and flushes on pause", () => {
		const onProgress = vi.fn();
		vi.spyOn(Date, "now").mockReturnValue(1_000);
		vidstackMock.player.paused = true;

		render(
			<WatchPlayer playlist={continuousPlaylist()} onProgress={onProgress} />,
		);

		fireEvent.click(screen.getByRole("button", { name: "play" }));
		vidstackMock.player.currentTime = 12;
		fireEvent.click(screen.getByRole("button", { name: "timeupdate" }));
		expect(onProgress).toHaveBeenCalledTimes(1);
		expect(onProgress).toHaveBeenLastCalledWith(12, false);

		vidstackMock.player.currentTime = 20;
		fireEvent.click(screen.getByRole("button", { name: "timeupdate" }));
		expect(onProgress).toHaveBeenCalledTimes(1);

		fireEvent.click(screen.getByRole("button", { name: "pause" }));
		expect(onProgress).toHaveBeenCalledTimes(2);
		expect(onProgress).toHaveBeenLastCalledWith(20, false);
	});

	it("emits completed progress at the end of continuous playback", () => {
		const onProgress = vi.fn();

		render(
			<WatchPlayer playlist={continuousPlaylist()} onProgress={onProgress} />,
		);
		fireEvent.click(screen.getByRole("button", { name: "ended" }));

		expect(onProgress).toHaveBeenCalledWith(120, true);
	});

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
		mockRect(screen.getByTestId("recording-timeline-rail"), {
			left: 0,
			width: 120,
		});

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
		mockRect(screen.getByTestId("recording-timeline-rail"), {
			left: 0,
			width: 120,
		});

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
		mockRect(screen.getByTestId("recording-timeline-rail"), {
			left: 0,
			width: 120,
		});

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

	it("uses compact audio controls for audio-only parts", () => {
		render(<WatchPlayer playlist={audioPlaylist()} />);

		expect(getAudioElement().getAttribute("src")).toBe("/part-1.m4a");
		expect(screen.getByTestId("audio-controls")).toBeTruthy();
		expect(
			document
				.querySelector(".rv-audio-time-readout")
				?.textContent?.replace(/\s+/g, " ")
				.trim(),
		).toBe("0:00 / 1:00");
		expect(screen.queryByTestId("video-layout")).toBeNull();
	});

	it("keeps the recording progress below the audio controls", () => {
		render(<WatchPlayer playlist={audioPlaylist()} />);

		const controls = screen.getByTestId("audio-controls");
		const progress = screen.getByRole("slider", {
			name: "watch.seek_recording",
		});

		expect(controls.contains(progress)).toBe(false);
		expect(
			controls.compareDocumentPosition(progress) &
				Node.DOCUMENT_POSITION_FOLLOWING,
		).toBeTruthy();
	});

	it("renders audio waveform peaks on the recording timeline", () => {
		render(
			<WatchPlayer
				playlist={audioPlaylist()}
				audioWaveform={{ peaks: [0, 0.35, 1, 0.5] }}
			/>,
		);

		expect(screen.getByTestId("audio-waveform")).toBeTruthy();
		expect(screen.getByTestId("audio-controls")).toBeTruthy();
		expect(
			screen.getByRole("slider", { name: "watch.seek_recording" }),
		).toBeTruthy();
	});

	it("seeks single-part audio from the recording slider after playback ends", () => {
		render(
			<WatchPlayer
				playlist={audioPlaylist()}
				audioWaveform={{ peaks: [0, 0.35, 1, 0.5] }}
			/>,
		);

		const audio = getAudioElement();
		fireEvent.canPlay(audio);
		fireEvent.ended(audio);

		const recordingSlider = screen.getByRole("slider", {
			name: "watch.seek_recording",
		});
		mockRect(screen.getByTestId("recording-timeline-rail"), {
			left: 0,
			width: 120,
		});

		fireEvent.pointerDown(recordingSlider, {
			button: 0,
			clientX: 30,
			pointerId: 1,
		});
		fireEvent.pointerUp(recordingSlider, {
			clientX: 30,
			pointerId: 1,
		});

		expect(audio.currentTime).toBe(15);
		expect(
			screen.queryByRole("button", { name: /watch\.part_segment:part=1/ }),
		).toBeNull();
	});

	it("lets the visible progress thumb seek away from the end of audio playback", () => {
		render(
			<WatchPlayer
				playlist={audioPlaylist()}
				audioWaveform={{ peaks: [0, 0.35, 1, 0.5] }}
			/>,
		);

		const audio = getAudioElement();
		fireEvent.canPlay(audio);
		fireEvent.ended(audio);

		const recordingSlider = screen.getByRole("slider", {
			name: "watch.seek_recording",
		});
		const thumb = screen.getByTestId("recording-progress-thumb");
		expect(thumb).toBeTruthy();
		mockRect(screen.getByTestId("recording-timeline-rail"), {
			left: 6,
			width: 108,
		});

		fireEvent.pointerDown(recordingSlider, {
			button: 0,
			clientX: 120,
			pointerId: 1,
		});
		fireEvent.pointerMove(recordingSlider, {
			clientX: 30,
			pointerId: 1,
		});
		fireEvent.pointerUp(recordingSlider, {
			clientX: 30,
			pointerId: 1,
		});

		expect(audio.currentTime).toBeCloseTo(13.33, 2);
	});

	it("keeps the dragged audio position when a stale ended time update arrives", () => {
		render(
			<WatchPlayer
				playlist={audioPlaylist()}
				audioWaveform={{ peaks: [0, 0.35, 1, 0.5] }}
			/>,
		);

		const audio = getAudioElement();
		fireEvent.canPlay(audio);
		fireEvent.ended(audio);

		const recordingSlider = screen.getByRole("slider", {
			name: "watch.seek_recording",
		});
		mockRect(screen.getByTestId("recording-timeline-rail"), {
			left: 0,
			width: 120,
		});

		fireEvent.pointerDown(recordingSlider, {
			button: 0,
			clientX: 120,
			pointerId: 1,
		});
		fireEvent.pointerMove(recordingSlider, {
			clientX: 30,
			pointerId: 1,
		});
		expect(recordingSlider.getAttribute("aria-valuenow")).toBe("15");

		audio.currentTime = 60;
		fireEvent.timeUpdate(audio);

		expect(recordingSlider.getAttribute("aria-valuenow")).toBe("15");
	});

	it("reapplies the committed audio seek before resuming from a stale ended state", async () => {
		const playSpy = vi
			.spyOn(window.HTMLMediaElement.prototype, "play")
			.mockResolvedValue(undefined);
		render(
			<WatchPlayer
				playlist={audioPlaylist()}
				audioWaveform={{ peaks: [0, 0.35, 1, 0.5] }}
			/>,
		);

		const audio = getAudioElement();
		fireEvent.canPlay(audio);
		fireEvent.ended(audio);

		const recordingSlider = screen.getByRole("slider", {
			name: "watch.seek_recording",
		});
		mockRect(screen.getByTestId("recording-timeline-rail"), {
			left: 0,
			width: 120,
		});

		fireEvent.click(recordingSlider, {
			clientX: 30,
		});
		expect(audio.currentTime).toBe(15);

		// The audio branch keeps the app's committed recording seek as the source
		// of truth, so Play reapplies it even if the native element has drifted.
		audio.currentTime = 0;
		fireEvent.click(screen.getByRole("button", { name: "Play" }));

		expect(audio.currentTime).toBe(15);
		expect(playSpy).toHaveBeenCalledTimes(1);
		playSpy.mockRestore();
	});

	it("replays audio from the recording start after natural playback ends", () => {
		const playSpy = vi
			.spyOn(window.HTMLMediaElement.prototype, "play")
			.mockResolvedValue(undefined);
		const nowSpy = vi.spyOn(Date, "now").mockReturnValue(1000);
		try {
			render(
				<WatchPlayer
					playlist={audioPlaylist()}
					audioWaveform={{ peaks: [0, 0.35, 1, 0.5] }}
				/>,
			);

			const audio = getAudioElement();
			fireEvent.canPlay(audio);
			fireEvent.ended(audio);
			audio.currentTime = 60;

			const recordingSlider = screen.getByRole("slider", {
				name: "watch.seek_recording",
			});
			expect(recordingSlider.getAttribute("aria-valuenow")).toBe("60");

			fireEvent.click(screen.getByRole("button", { name: "Play" }));

			expect(audio.currentTime).toBe(0);
			expect(recordingSlider.getAttribute("aria-valuenow")).toBe("0");
			expect(playSpy).toHaveBeenCalledTimes(1);

			nowSpy.mockReturnValue(3000);
			audio.currentTime = 2;
			fireEvent.timeUpdate(audio);

			expect(recordingSlider.getAttribute("aria-valuenow")).toBe("2");
		} finally {
			nowSpy.mockRestore();
			playSpy.mockRestore();
		}
	});

	it("changes audio volume through the range control", () => {
		render(<WatchPlayer playlist={audioPlaylist()} />);

		const volumeSlider = screen.getByRole("slider", { name: "Volume" });
		fireEvent.change(volumeSlider, { target: { value: "0.35" } });

		const audio = getAudioElement();
		expect(audio.volume).toBe(0.35);
		expect(audio.muted).toBe(false);
	});

	it("toggles audio mute through the custom volume button", () => {
		render(<WatchPlayer playlist={audioPlaylist()} />);

		fireEvent.click(screen.getByRole("button", { name: "Mute" }));

		expect(getAudioElement().muted).toBe(true);
	});

	it("uses compact audio controls for ready audio playback artifacts", () => {
		render(
			<WatchPlayer
				playlist={{
					...audioPlaylist(),
					continuousSource: {
						src: "/api/v1/videos/65/playback/stream",
						mimeType: "audio/mp4",
						durationSeconds: null,
					},
				}}
			/>,
		);

		expect(getAudioElement().getAttribute("src")).toBe(
			"/api/v1/videos/65/playback/stream",
		);
		expect(screen.getByTestId("audio-controls")).toBeTruthy();
		expect(screen.queryByTestId("video-layout")).toBeNull();
	});

	it("renders waveform peaks through the last bucket with no empty tail", () => {
		render(
			<WatchPlayer
				playlist={audioPlaylist()}
				audioWaveform={{ peaks: [0, 0.35, 1, 0.5] }}
			/>,
		);

		const waveform = screen.getByTestId("audio-waveform");
		const svg = waveform.querySelector("svg");
		const path = waveform.querySelector("path");
		expect(svg?.getAttribute("viewBox")).toBe("0 0 3 100");
		expect(path?.getAttribute("d")).toContain("M 3 ");
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

describe("WatchPlayer watch progress persistence", () => {
	function hide(hidden: boolean) {
		vi.spyOn(document, "visibilityState", "get").mockReturnValue(
			hidden ? "hidden" : "visible",
		);
		act(() => {
			document.dispatchEvent(new Event("visibilitychange"));
		});
	}

	it("flushes the exact position when the tab hides and again on unmount", () => {
		const onProgress = vi.fn();
		vi.spyOn(Date, "now").mockReturnValue(1_000);
		vidstackMock.player.paused = true;
		const { unmount } = render(
			<WatchPlayer playlist={continuousPlaylist()} onProgress={onProgress} />,
		);

		fireEvent.click(screen.getByRole("button", { name: "play" }));
		vidstackMock.player.currentTime = 12;
		fireEvent.click(screen.getByRole("button", { name: "timeupdate" }));
		expect(onProgress).toHaveBeenCalledTimes(1);

		// Inside the throttle window: the periodic save holds back.
		vidstackMock.player.currentTime = 20;
		fireEvent.click(screen.getByRole("button", { name: "timeupdate" }));
		expect(onProgress).toHaveBeenCalledTimes(1);

		hide(true);
		expect(onProgress).toHaveBeenCalledTimes(2);
		expect(onProgress).toHaveBeenLastCalledWith(20, false);

		// Hidden again without moving: nothing new to save.
		hide(true);
		expect(onProgress).toHaveBeenCalledTimes(2);

		// Coming back is not a save point.
		hide(false);
		expect(onProgress).toHaveBeenCalledTimes(2);

		vidstackMock.player.currentTime = 25;
		fireEvent.click(screen.getByRole("button", { name: "timeupdate" }));
		expect(onProgress).toHaveBeenCalledTimes(2);
		unmount();
		expect(onProgress).toHaveBeenCalledTimes(3);
		expect(onProgress).toHaveBeenLastCalledWith(25, false);
	});

	it("preserves progress when the recording duration is unknown", () => {
		const onProgress = vi.fn();
		const playlist = continuousPlaylist();
		playlist.totalDurationSeconds = 0;
		vidstackMock.player.paused = true;
		const { unmount } = render(
			<WatchPlayer playlist={playlist} onProgress={onProgress} />,
		);
		fireEvent.click(screen.getByRole("button", { name: "play" }));
		vidstackMock.player.currentTime = 42;
		fireEvent.click(screen.getByRole("button", { name: "timeupdate" }));
		unmount();
		expect(onProgress).toHaveBeenLastCalledWith(42, false);
	});

	it("flushes the position on pagehide", () => {
		const onProgress = vi.fn();
		vi.spyOn(Date, "now").mockReturnValue(1_000);
		vidstackMock.player.paused = true;
		render(
			<WatchPlayer playlist={continuousPlaylist()} onProgress={onProgress} />,
		);

		fireEvent.click(screen.getByRole("button", { name: "play" }));
		vidstackMock.player.currentTime = 12;
		fireEvent.click(screen.getByRole("button", { name: "timeupdate" }));
		vidstackMock.player.currentTime = 20;
		fireEvent.click(screen.getByRole("button", { name: "timeupdate" }));
		expect(onProgress).toHaveBeenCalledTimes(1);

		act(() => {
			window.dispatchEvent(new Event("pagehide"));
		});
		expect(onProgress).toHaveBeenCalledTimes(2);
		expect(onProgress).toHaveBeenLastCalledWith(20, false);
	});

	it("does not write back a seed the viewer never played", () => {
		const onProgress = vi.fn();
		vidstackMock.player.paused = true;
		const { unmount } = render(
			<WatchPlayer
				playlist={continuousPlaylist()}
				initialOffsetSeconds={70}
				onProgress={onProgress}
			/>,
		);
		fireEvent.click(screen.getByRole("button", { name: "canplay" }));
		expect(vidstackMock.player.currentTime).toBe(70);

		hide(true);
		act(() => {
			window.dispatchEvent(new Event("pagehide"));
		});
		unmount();
		expect(onProgress).not.toHaveBeenCalled();
	});

	it("saves a deliberate return to the start when the viewer leaves", () => {
		const onProgress = vi.fn();
		vi.spyOn(Date, "now").mockReturnValue(1_000);
		vidstackMock.player.paused = true;
		const { unmount } = render(
			<WatchPlayer playlist={continuousPlaylist()} onProgress={onProgress} />,
		);
		fireEvent.click(screen.getByRole("button", { name: "canplay" }));
		fireEvent.click(screen.getByRole("button", { name: "play" }));
		vidstackMock.player.currentTime = 40;
		fireEvent.click(screen.getByRole("button", { name: "timeupdate" }));
		expect(onProgress).toHaveBeenLastCalledWith(40, false);

		fireEvent.keyDown(screen.getByTestId("media-player"), { key: "Home" });
		unmount();
		expect(onProgress).toHaveBeenLastCalledWith(0, false);
	});

	it("does not save a paused seek twice", () => {
		const onProgress = vi.fn();
		vi.spyOn(Date, "now").mockReturnValue(1_000);
		vidstackMock.player.paused = true;
		render(
			<WatchPlayer playlist={continuousPlaylist()} onProgress={onProgress} />,
		);
		fireEvent.click(screen.getByRole("button", { name: "canplay" }));
		fireEvent.click(screen.getByRole("button", { name: "play" }));
		vidstackMock.player.currentTime = 30;
		fireEvent.click(screen.getByRole("button", { name: "timeupdate" }));
		fireEvent.click(screen.getByRole("button", { name: "pause" }));
		expect(onProgress).toHaveBeenCalledTimes(1);

		// Seeking while paused moves the place to save; hiding saves it once.
		fireEvent.keyDown(screen.getByTestId("media-player"), { key: "PageUp" });
		hide(true);
		expect(onProgress).toHaveBeenCalledTimes(2);
		expect(onProgress).toHaveBeenLastCalledWith(90, false);
		hide(true);
		expect(onProgress).toHaveBeenCalledTimes(2);
	});

	it("applies the initial offset under StrictMode's double effect pass", () => {
		vidstackMock.player.paused = true;
		render(
			<StrictMode>
				<WatchPlayer
					playlist={continuousPlaylist()}
					initialOffsetSeconds={70}
				/>
			</StrictMode>,
		);

		expect(
			screen
				.getByRole("slider", { name: "watch.seek_recording" })
				.getAttribute("aria-valuenow"),
		).toBe("70");
		fireEvent.click(screen.getByRole("button", { name: "canplay" }));
		expect(vidstackMock.player.currentTime).toBe(70);
		expect(vidstackMock.player.play).not.toHaveBeenCalled();
	});

	it("keeps a resumed seek the audio element ignored and retries at the next readiness event", () => {
		render(
			<WatchPlayer playlist={audioPlaylist()} initialOffsetSeconds={30} />,
		);
		const audio = getAudioElement();
		let ignoring = true;
		let stored = 0;
		Object.defineProperty(audio, "currentTime", {
			configurable: true,
			get: () => stored,
			set: (value: number) => {
				if (!ignoring) stored = value;
			},
		});

		fireEvent.loadedMetadata(audio);
		expect(audio.currentTime).toBe(0);

		ignoring = false;
		fireEvent.canPlay(audio);
		expect(audio.currentTime).toBe(30);
		expect(audio.paused).toBe(true);
	});

	it("shows where playback resumed and starts over on request", () => {
		vidstackMock.player.paused = true;
		render(
			<WatchPlayer
				playlist={continuousPlaylist()}
				initialOffsetSeconds={70}
				resumedFromSeconds={70}
			/>,
		);
		fireEvent.click(screen.getByRole("button", { name: "canplay" }));
		expect(vidstackMock.player.currentTime).toBe(70);
		expect(screen.getByTestId("resume-notice").textContent).toContain(
			`watch.resumed_from:time=${formatPlaybackTime(70)}`,
		);

		fireEvent.click(screen.getByRole("button", { name: "watch.start_over" }));
		expect(vidstackMock.player.currentTime).toBe(0);
		expect(
			screen
				.getByRole("slider", { name: "watch.seek_recording" })
				.getAttribute("aria-valuenow"),
		).toBe("0");
		expect(screen.queryByTestId("resume-notice")).toBeNull();
		expect(vidstackMock.player.play).not.toHaveBeenCalled();
	});

	it("hides the resume notice on dismiss and on its own after a while", () => {
		vi.useFakeTimers();
		vidstackMock.player.paused = true;
		const { unmount } = render(
			<WatchPlayer
				playlist={continuousPlaylist()}
				initialOffsetSeconds={70}
				resumedFromSeconds={70}
			/>,
		);
		expect(screen.getByTestId("resume-notice")).toBeTruthy();
		act(() => {
			vi.advanceTimersByTime(8_000);
		});
		expect(screen.queryByTestId("resume-notice")).toBeNull();
		unmount();

		render(
			<WatchPlayer
				playlist={continuousPlaylist()}
				initialOffsetSeconds={70}
				resumedFromSeconds={70}
			/>,
		);
		fireEvent.click(
			screen.getByRole("button", { name: "watch.dismiss_resume" }),
		);
		expect(screen.queryByTestId("resume-notice")).toBeNull();
		// Dismissing is not a seek: the saved place stays.
		fireEvent.click(screen.getByRole("button", { name: "canplay" }));
		expect(vidstackMock.player.currentTime).toBe(70);
	});

	it("shows no notice for a deep link", () => {
		render(
			<WatchPlayer playlist={continuousPlaylist()} initialOffsetSeconds={70} />,
		);
		expect(screen.queryByTestId("resume-notice")).toBeNull();
	});

	it("resumes into a later part from a saved offset without autoplay", () => {
		vidstackMock.player.paused = true;
		render(
			<WatchPlayer playlist={multipartPlaylist()} initialOffsetSeconds={75} />,
		);

		expect(screen.getByTestId("media-player").getAttribute("data-src")).toBe(
			"/part-2.mp4",
		);
		expect(vidstackMock.player.currentTime).toBe(0);
		fireEvent.click(screen.getByRole("button", { name: "canplay" }));
		expect(vidstackMock.player.currentTime).toBe(15);
		expect(vidstackMock.player.play).not.toHaveBeenCalled();
		expect(screen.getByText(/watch\.part_status:current=2/)).toBeTruthy();
	});

	it("applies a saved offset to audio as soon as its metadata is loaded", () => {
		render(
			<WatchPlayer playlist={audioPlaylist()} initialOffsetSeconds={30} />,
		);
		const audio = getAudioElement();
		expect(audio.currentTime).toBe(0);

		fireEvent.loadedMetadata(audio);
		expect(audio.currentTime).toBe(30);
		expect(audio.paused).toBe(true);

		// canplay after metadata must not seek again or start playback.
		audio.currentTime = 31;
		fireEvent.canPlay(audio);
		expect(audio.currentTime).toBe(31);
		expect(audio.paused).toBe(true);
	});
});

function multipartPlaylist(): RecordingPlaylist {
	return {
		videoId: 65,
		title: "Recording",
		isAudioOnly: false,
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

function audioPlaylist(): RecordingPlaylist {
	return {
		...multipartPlaylist(),
		isAudioOnly: true,
		continuousSource: null,
		totalDurationSeconds: 60,
		parts: [
			{
				partIndex: 1,
				position: 0,
				src: "/part-1.m4a",
				mimeType: "audio/mp4",
				durationSeconds: 60,
				sizeBytes: 100,
				startSeconds: 0,
				endSeconds: 60,
				label: "Part 1",
			},
		],
	};
}

function getAudioElement() {
	const audio = document.querySelector("audio");
	expect(audio).not.toBeNull();
	return audio as HTMLAudioElement;
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

function singlePartPlaylist(): RecordingPlaylist {
	const base = multipartPlaylist();
	return {
		...base,
		totalDurationSeconds: 60,
		parts: base.parts.slice(0, 1),
		markers: [],
	};
}

function stubProbe(status: number | Error) {
	const probe = vi.fn(async () => {
		if (status instanceof Error) throw status;
		return { ok: status >= 200 && status < 300, status } as Response;
	});
	vi.stubGlobal("fetch", probe);
	return probe;
}

describe("WatchPlayer unavailable media", () => {
	it("replaces the player with a file-missing panel when the source is gone", async () => {
		const probe = stubProbe(404);
		const onMediaUnavailable = vi.fn();

		render(
			<WatchPlayer
				playlist={singlePartPlaylist()}
				onMediaUnavailable={onMediaUnavailable}
				unavailableActions={<span>remove-action</span>}
			/>,
		);
		fireEvent.click(screen.getByRole("button", { name: "error" }));

		const panel = await screen.findByTestId("media-unavailable");
		expect(panel.getAttribute("data-kind")).toBe("gone");
		expect(panel.textContent).toContain("watch.media_gone_title");
		expect(panel.textContent).toContain("remove-action");
		expect(screen.queryByTestId("media-player")).toBeNull();
		expect(onMediaUnavailable).toHaveBeenCalledWith("gone");
		expect(probe).toHaveBeenCalledWith(
			"/part-1.mp4",
			expect.objectContaining({ method: "HEAD" }),
		);
	});

	it("offers a retry that reloads the source after a transient failure", async () => {
		stubProbe(new Error("network down"));
		const onMediaUnavailable = vi.fn();

		render(
			<WatchPlayer
				playlist={singlePartPlaylist()}
				onMediaUnavailable={onMediaUnavailable}
				unavailableActions={<span>remove-action</span>}
			/>,
		);
		fireEvent.click(screen.getByRole("button", { name: "error" }));

		const panel = await screen.findByTestId("media-unavailable");
		expect(panel.getAttribute("data-kind")).toBe("failed");
		expect(panel.textContent).not.toContain("remove-action");
		expect(onMediaUnavailable).not.toHaveBeenCalled();

		fireEvent.click(screen.getByRole("button", { name: "watch.retry" }));

		expect(screen.queryByTestId("media-unavailable")).toBeNull();
		expect(screen.getByTestId("media-player").getAttribute("data-src")).toBe(
			"/part-1.mp4",
		);
	});

	it("keeps the continuous-to-parts fallback ahead of the panel", async () => {
		const probe = stubProbe(404);

		render(<WatchPlayer playlist={continuousPlaylist()} />);
		fireEvent.click(screen.getByRole("button", { name: "error" }));

		expect(screen.getByTestId("media-player").getAttribute("data-src")).toBe(
			"/part-1.mp4",
		);
		await waitFor(() => expect(probe).not.toHaveBeenCalled());
		expect(screen.queryByTestId("media-unavailable")).toBeNull();
	});

	it("shows the panel for an audio source that cannot load", async () => {
		stubProbe(410);
		const onMediaUnavailable = vi.fn();

		render(
			<WatchPlayer
				playlist={audioPlaylist()}
				onMediaUnavailable={onMediaUnavailable}
			/>,
		);
		fireEvent.error(getAudioElement());

		const panel = await screen.findByTestId("media-unavailable");
		expect(panel.getAttribute("data-kind")).toBe("removed");
		expect(screen.queryByTestId("audio-controls")).toBeNull();
		expect(screen.queryByRole("button", { name: "watch.retry" })).toBeNull();
		expect(onMediaUnavailable).toHaveBeenCalledWith("removed");
	});

	it("probes a source that never becomes playable and surfaces a gone file", async () => {
		vi.useFakeTimers();
		const probe = stubProbe(404);
		const onMediaUnavailable = vi.fn();

		render(
			<WatchPlayer
				playlist={singlePartPlaylist()}
				onMediaUnavailable={onMediaUnavailable}
			/>,
		);
		await vi.advanceTimersByTimeAsync(19_000);
		expect(probe).not.toHaveBeenCalled();

		await vi.advanceTimersByTimeAsync(2_000);
		expect(probe).toHaveBeenCalledTimes(1);
		// The probe resolves on the microtask queue and React flushes the state
		// change on the faked scheduler, so drain both before reading the DOM.
		await act(async () => {
			await vi.advanceTimersByTimeAsync(50);
		});
		expect(
			screen.getByTestId("media-unavailable").getAttribute("data-kind"),
		).toBe("gone");
		expect(onMediaUnavailable).toHaveBeenCalledWith("gone");
	});

	it("does not probe a source that already reached canplay", async () => {
		vi.useFakeTimers();
		const probe = stubProbe(404);

		render(<WatchPlayer playlist={singlePartPlaylist()} />);
		fireEvent.click(screen.getByRole("button", { name: "canplay" }));
		await vi.advanceTimersByTimeAsync(25_000);

		expect(probe).not.toHaveBeenCalled();
		expect(screen.queryByTestId("media-unavailable")).toBeNull();
	});

	it("starts a full watchdog deadline for a new source", async () => {
		vi.useFakeTimers();
		const probe = stubProbe(404);
		render(<WatchPlayer playlist={multipartPlaylist()} />);
		await act(() => vi.advanceTimersByTimeAsync(16_000));
		fireEvent.click(screen.getByRole("button", { name: /Second title/ }));
		await act(() => vi.advanceTimersByTimeAsync(19_999));
		expect(probe).not.toHaveBeenCalled();
		await act(() => vi.advanceTimersByTimeAsync(1));
		expect(probe).toHaveBeenCalledExactlyOnceWith(
			"/part-2.mp4",
			expect.objectContaining({ method: "HEAD" }),
		);
	});

	it("cancels the watchdog when the player unmounts", async () => {
		vi.useFakeTimers();
		const probe = stubProbe(404);
		const onMediaUnavailable = vi.fn();
		const { unmount } = render(
			<WatchPlayer
				playlist={singlePartPlaylist()}
				onMediaUnavailable={onMediaUnavailable}
			/>,
		);
		await act(() => vi.advanceTimersByTimeAsync(16_000));
		unmount();
		await act(() => vi.advanceTimersByTimeAsync(20_000));
		expect(probe).not.toHaveBeenCalled();
		expect(onMediaUnavailable).not.toHaveBeenCalled();
	});

	it.each([
		["video", singlePartPlaylist, 404, "gone"],
		["audio", audioPlaylist, 410, "removed"],
	] as const)("keeps the %s watchdog deadline through callback changes and notifies the latest callback", async (_name, makePlaylist, status, kind) => {
		vi.useFakeTimers();
		const probe = stubProbe(status);
		const playlist = makePlaylist();
		const callbacks = Array.from({ length: 5 }, () => vi.fn());
		const { rerender } = render(
			<StrictMode>
				<WatchPlayer playlist={playlist} onMediaUnavailable={callbacks[0]} />
			</StrictMode>,
		);
		for (let i = 1; i < callbacks.length; i++) {
			await act(() => vi.advanceTimersByTimeAsync(4_000));
			rerender(
				<StrictMode>
					<WatchPlayer
						playlist={{ ...playlist }}
						onMediaUnavailable={callbacks[i]}
					/>
				</StrictMode>,
			);
		}
		await act(() => vi.advanceTimersByTimeAsync(3_999));
		expect(probe).not.toHaveBeenCalled();
		await act(() => vi.advanceTimersByTimeAsync(1));
		expect(probe).toHaveBeenCalledTimes(1);
		expect(
			screen.getByTestId("media-unavailable").getAttribute("data-kind"),
		).toBe(kind);
		expect(callbacks[4]).toHaveBeenCalledExactlyOnceWith(kind);
		for (const previous of callbacks.slice(0, -1)) {
			expect(previous).not.toHaveBeenCalled();
		}
	});
});

describe("WatchPlayer stale probe results", () => {
	function deferredProbe() {
		let resolve: (status: number) => void = () => {};
		const probe = vi.fn(
			() =>
				new Promise<Response>((done) => {
					resolve = (status) => done({ ok: false, status } as Response);
				}),
		);
		vi.stubGlobal("fetch", probe);
		return { probe, resolve: (status: number) => resolve(status) };
	}

	it("delivers an in-flight watchdog result to the latest callback", async () => {
		vi.useFakeTimers();
		const { probe, resolve } = deferredProbe();
		const previous = vi.fn();
		const latest = vi.fn();
		const playlist = singlePartPlaylist();
		const { rerender } = render(
			<WatchPlayer playlist={playlist} onMediaUnavailable={previous} />,
		);
		await act(() => vi.advanceTimersByTimeAsync(20_000));
		expect(probe).toHaveBeenCalledTimes(1);
		rerender(<WatchPlayer playlist={playlist} onMediaUnavailable={latest} />);
		await act(async () => resolve(404));
		expect(latest).toHaveBeenCalledExactlyOnceWith("gone");
		expect(previous).not.toHaveBeenCalled();
		expect(screen.getByTestId("media-unavailable")).toBeTruthy();
	});

	it("drops an in-flight watchdog result after unmount", async () => {
		vi.useFakeTimers();
		const { probe, resolve } = deferredProbe();
		const onMediaUnavailable = vi.fn();
		const { unmount } = render(
			<WatchPlayer
				playlist={singlePartPlaylist()}
				onMediaUnavailable={onMediaUnavailable}
			/>,
		);
		await act(() => vi.advanceTimersByTimeAsync(20_000));
		expect(probe).toHaveBeenCalledTimes(1);
		unmount();
		await act(async () => resolve(404));
		expect(onMediaUnavailable).not.toHaveBeenCalled();
	});

	it("drops an error probe that resolves after the recording changed", async () => {
		const { probe, resolve } = deferredProbe();
		const onMediaUnavailable = vi.fn();
		const { rerender } = render(
			<WatchPlayer
				playlist={singlePartPlaylist()}
				onMediaUnavailable={onMediaUnavailable}
			/>,
		);
		fireEvent.click(screen.getByRole("button", { name: "error" }));
		expect(probe).toHaveBeenCalledTimes(1);

		// A different recording with a different source URL takes over before
		// the probe for the first one comes back.
		rerender(
			<WatchPlayer
				playlist={{ ...continuousPlaylist(), videoId: 66 }}
				onMediaUnavailable={onMediaUnavailable}
			/>,
		);
		await act(async () => {
			resolve(404);
		});

		expect(screen.queryByTestId("media-unavailable")).toBeNull();
		expect(screen.getByTestId("media-player").getAttribute("data-src")).toBe(
			"/api/v1/videos/65/playback/stream",
		);
		expect(onMediaUnavailable).not.toHaveBeenCalled();
	});

	it("drops a watchdog probe that resolves after the source changed", async () => {
		vi.useFakeTimers();
		const { probe, resolve } = deferredProbe();
		const onMediaUnavailable = vi.fn();
		const { rerender } = render(
			<WatchPlayer
				playlist={singlePartPlaylist()}
				onMediaUnavailable={onMediaUnavailable}
			/>,
		);
		await vi.advanceTimersByTimeAsync(21_000);
		expect(probe).toHaveBeenCalledTimes(1);

		rerender(
			<WatchPlayer
				playlist={{ ...continuousPlaylist(), videoId: 66 }}
				onMediaUnavailable={onMediaUnavailable}
			/>,
		);
		await act(async () => {
			resolve(410);
			await vi.advanceTimersByTimeAsync(50);
		});

		expect(screen.queryByTestId("media-unavailable")).toBeNull();
		expect(onMediaUnavailable).not.toHaveBeenCalled();
	});
});

describe("missing-media recovery", () => {
	afterEach(() => {
		vi.unstubAllGlobals();
		vi.useRealTimers();
	});

	it("deduplicates errors and aborts a probe when the source changes", async () => {
		let finish!: (value: Response) => void;
		const fetchImpl = vi.fn(
			() =>
				new Promise<Response>((resolve) => {
					finish = resolve;
				}),
		);
		vi.stubGlobal("fetch", fetchImpl);
		const unavailable = vi.fn();
		const { rerender } = render(
			<WatchPlayer
				playlist={audioPlaylist()}
				onMediaUnavailable={unavailable}
			/>,
		);
		fireEvent.error(getAudioElement());
		fireEvent.error(getAudioElement());
		expect(fetchImpl).toHaveBeenCalledTimes(1);
		const signal = (
			fetchImpl.mock.calls[0] as unknown as [string, RequestInit]
		)[1].signal;
		const next = audioPlaylist();
		next.parts = [{ ...next.parts[0], src: "/replacement.m4a" }];
		rerender(<WatchPlayer playlist={next} onMediaUnavailable={unavailable} />);
		expect(signal?.aborted).toBe(true);
		await act(async () => finish({ ok: false, status: 404 } as Response));
		expect(screen.queryByTestId("media-unavailable")).toBeNull();
		expect(unavailable).not.toHaveBeenCalled();
	});

	it("aborts in-flight work when the player unmounts", async () => {
		let finish!: (value: Response) => void;
		const fetchImpl = vi.fn(
			() =>
				new Promise<Response>((resolve) => {
					finish = resolve;
				}),
		);
		vi.stubGlobal("fetch", fetchImpl);
		const unavailable = vi.fn();
		const { unmount } = render(
			<WatchPlayer
				playlist={audioPlaylist()}
				onMediaUnavailable={unavailable}
			/>,
		);
		fireEvent.error(getAudioElement());
		const signal = (
			fetchImpl.mock.calls[0] as unknown as [string, RequestInit]
		)[1].signal;
		unmount();
		expect(signal?.aborted).toBe(true);
		await act(async () => finish({ ok: false, status: 404 } as Response));
		expect(unavailable).not.toHaveBeenCalled();
	});

	it("shows a retryable failure for a stalled source even if no native error arrives", async () => {
		vi.useFakeTimers();
		vi.stubGlobal(
			"fetch",
			vi.fn(async () => ({ ok: false, status: 503 }) as Response),
		);
		const { rerender } = render(
			<WatchPlayer playlist={audioPlaylist()} onMediaUnavailable={() => {}} />,
		);
		await act(async () => {
			await vi.advanceTimersByTimeAsync(15_000);
		});
		// An unstable route callback must not postpone the source's load deadline.
		rerender(
			<WatchPlayer playlist={audioPlaylist()} onMediaUnavailable={() => {}} />,
		);
		await act(async () => {
			await vi.advanceTimersByTimeAsync(5_000);
		});
		expect(
			screen.getByTestId("media-unavailable").getAttribute("data-kind"),
		).toBe("failed");
		fireEvent.click(screen.getByRole("button", { name: "watch.retry" }));
		expect(screen.queryByTestId("media-unavailable")).toBeNull();
		expect(getAudioElement()).not.toBeNull();
	});
});

describe("retry playback position", () => {
	it("restores the viewer's position when the replacement media element becomes ready", async () => {
		stubProbe(503);
		render(<WatchPlayer playlist={audioPlaylist()} />);
		const audio = getAudioElement();
		fireEvent.canPlay(audio);
		audio.currentTime = 17;
		fireEvent.timeUpdate(audio);
		fireEvent.error(audio);
		await screen.findByTestId("media-unavailable");
		fireEvent.click(screen.getByRole("button", { name: "watch.retry" }));
		const replacement = getAudioElement();
		expect(replacement).not.toBe(audio);
		fireEvent.canPlay(replacement);
		expect(replacement.currentTime).toBe(17);
	});
});

describe("fullscreen orientation lock", () => {
	function stubHandheld(matches: boolean) {
		const listeners = new Set<() => void>();
		const query = {
			matches,
			addEventListener: (_type: "change", listener: () => void) => {
				listeners.add(listener);
			},
			removeEventListener: (_type: "change", listener: () => void) => {
				listeners.delete(listener);
			},
			set(next: boolean) {
				query.matches = next;
				for (const listener of listeners) listener();
			},
		};
		vi.stubGlobal(
			"matchMedia",
			vi.fn(() => query),
		);
		vi.stubGlobal("screen", {
			orientation: {
				lock: () => Promise.resolve(),
				unlock: () => Promise.resolve(),
			},
		});
		return query;
	}

	function lockType() {
		return screen
			.getByTestId("media-player")
			.getAttribute("data-fullscreen-orientation");
	}

	// jsdom has neither matchMedia nor screen.orientation.lock, which is the
	// same shape as a browser that cannot honour the lock.
	it("leaves the screen alone where the browser cannot lock it", () => {
		render(<WatchPlayer playlist={continuousPlaylist()} />);
		expect(lockType()).toBe("none");
	});

	it("locks landscape on a handheld that exposes the orientation API", () => {
		stubHandheld(true);
		render(<WatchPlayer playlist={continuousPlaylist()} />);
		expect(lockType()).toBe("landscape");
	});

	it("keeps the lock type steady until the player leaves fullscreen", () => {
		const query = stubHandheld(true);
		render(<WatchPlayer playlist={continuousPlaylist()} />);
		fireEvent.click(screen.getByRole("button", { name: "enter fullscreen" }));
		act(() => query.set(false));
		expect(lockType()).toBe("landscape");
		fireEvent.click(screen.getByRole("button", { name: "exit fullscreen" }));
		expect(lockType()).toBe("none");
	});
});
