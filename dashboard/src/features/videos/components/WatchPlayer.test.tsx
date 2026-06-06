// @vitest-environment jsdom

import {
	cleanup,
	fireEvent,
	render,
	screen,
	waitFor,
} from "@testing-library/react";
import type { ButtonHTMLAttributes, ReactNode } from "react";
import { afterEach, describe, expect, it, vi } from "vitest";
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
					"data-type": sourceType(src),
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
					onClick: (event) => {
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
