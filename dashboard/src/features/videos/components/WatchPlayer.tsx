import "@vidstack/react/player/styles/default/theme.css";
import "@vidstack/react/player/styles/default/layouts/video.css";
import "./WatchPlayer.css";

import {
	FastForwardIcon,
	PauseIcon,
	PlayIcon,
	RewindIcon,
	SpeakerHighIcon,
	SpeakerSlashIcon,
} from "@phosphor-icons/react";
import {
	MediaPlayer,
	type MediaPlayerInstance,
	MediaProvider,
	type PlayerSrc,
	Track,
	useMediaRemote,
	useMediaState,
	type VTTContent,
} from "@vidstack/react";
import {
	DefaultVideoLayout,
	defaultLayoutIcons,
} from "@vidstack/react/player/layouts/default";
import {
	type ChangeEvent,
	type CSSProperties,
	type KeyboardEvent,
	type MouseEvent,
	type PointerEvent,
	useCallback,
	useEffect,
	useMemo,
	useRef,
	useState,
} from "react";
import { useTranslation } from "react-i18next";
import { API_URL } from "@/env";
import {
	TimelineChangeContent,
	TimelinePartContent,
} from "@/features/videos/components/timelinePopover";
import { formatBytes, formatPlaybackTime } from "@/features/videos/format";
import {
	chapterCuesForPart,
	chapterCuesForRecording,
	findPartForOffset,
	globalTimeForPart,
	type RecordingPlaylist,
	type RecordingPlaylistPart,
	type RecordingTimelineMarker,
} from "@/features/videos/playback";
import { clamp, cn, percentOf, popoverAlign } from "@/lib/utils";

type RecordingSeekMode = "preview" | "commit";

type RecordingSeekOptions = {
	resume?: boolean;
	trigger?: Event;
	mode?: RecordingSeekMode;
};

type CommittedRecordingSeek = {
	canonicalSeconds: number;
	sourceKey: string;
	sourceSeconds: number;
};

// WatchPlayer wraps Vidstack's MediaPlayer with the app's defaults so
// the route can pass one recording-level playlist. Multipart recordings
// sequence parts in this wrapper with a recording-level time slider that knows
// about part boundaries and metadata markers. A ready continuous source can be
// used when the API exposes one, and media errors fall back to part sequencing.
//
// `crossOrigin` is only set when the API runs on a different origin
// (VITE_API_URL non-empty). Setting `use-credentials` on a same-
// origin URL forces CORS mode and the request fails unless the
// server returns Access-Control-Allow-Credentials, which it
// doesn't bother to do for same-origin in dev. When VITE_API_URL is
// configured we're cross-origin and the streaming middleware does
// emit the credential-allowing CORS headers.
export function WatchPlayer({
	audioWaveform,
	audioWaveformLoading,
	playlist,
	initialOffsetSeconds,
}: {
	audioWaveform?: { peaks: number[] } | null;
	audioWaveformLoading?: boolean;
	playlist: RecordingPlaylist;
	initialOffsetSeconds?: number;
}) {
	const isCrossOrigin = !!API_URL;
	const playerRef = useRef<MediaPlayerInstance>(null);
	const audioRef = useRef<HTMLAudioElement>(null);
	const pendingSeekRef = useRef<{
		localSeconds: number;
		playbackRate: number;
		resume: boolean;
	} | null>(null);
	const appliedInitialSeekRef = useRef<string | null>(null);
	const handledEndedSourceRef = useRef<string | null>(null);
	const pendingUserSeekRef = useRef<{
		canonicalSeconds: number;
		expiresAt: number;
		sourceKey: string;
	} | null>(null);
	const committedSeekRef = useRef<CommittedRecordingSeek | null>(null);
	const wasPlayingRef = useRef(false);
	const mediaRemote = useMediaRemote(playerRef);
	// Tracks the previous continuous-vs-parts mode so the effect below can detect
	// a parts → single-file upgrade (the lazily-built artifact appearing
	// mid-session) and resume playback at the current spot rather than restart.
	const prevUsesContinuousRef = useRef(playlist.continuousSource != null);
	// readySourceKeyRef holds the source identity that has fired canplay and is
	// therefore safe for an immediate currentTime assignment. Until the loaded
	// source matches it (cold load / deep link / source swap) seekToGlobal defers
	// to pendingSeekRef and handleCanPlay applies it. Keying readiness on identity
	// flips it the instant the source changes — no reset effect needed.
	const readySourceKeyRef = useRef<string | undefined>(undefined);
	const [partPosition, setPartPosition] = useState(0);
	const [globalTime, setGlobalTime] = useState(0);
	const [forcePartSequencer, setForcePartSequencer] = useState(false);
	const [audioMuted, setAudioMuted] = useState(false);
	const [audioPaused, setAudioPaused] = useState(true);
	const [audioPlaybackRate, setAudioPlaybackRate] = useState(1);
	const [audioVolume, setAudioVolume] = useState(1);

	// Use the continuous single-file source whenever the API exposes one. It's
	// built lazily (the first play kicks the concat), so it can appear partway
	// through a session; the effect below swaps to it without losing the user's
	// place. The only reason to ignore an available continuous source is a media
	// error that forced the part sequencer for this recording.
	const usesContinuousSource =
		playlist.continuousSource != null && !forcePartSequencer;
	const currentPart = usesContinuousSource
		? (findPartForOffset(
				playlist.parts,
				globalTime,
				playlist.totalDurationSeconds,
			)?.part ?? playlist.parts[0])
		: (playlist.parts[partPosition] ?? playlist.parts[0]);
	const chapterCues = useMemo(
		() =>
			usesContinuousSource
				? chapterCuesForRecording(
						playlist.markers,
						playlist.totalDurationSeconds,
						playlist.title,
					)
				: currentPart
					? chapterCuesForPart(playlist.markers, currentPart)
					: [],
		[
			currentPart,
			playlist.markers,
			playlist.title,
			playlist.totalDurationSeconds,
			usesContinuousSource,
		],
	);
	const chapterContent = useMemo<VTTContent>(
		() => ({ cues: chapterCues }),
		[chapterCues],
	);
	const currentSource = useMemo<PlayerSrc | undefined>(
		() =>
			usesContinuousSource && playlist.continuousSource
				? ({
						src: playlist.continuousSource.src,
						type: playlist.isAudioOnly
							? "audio/mp4"
							: playlist.continuousSource.mimeType,
					} as PlayerSrc)
				: currentPart
					? ({
							src: currentPart.src,
							type: playlist.isAudioOnly ? "audio/mp4" : currentPart.mimeType,
						} as PlayerSrc)
					: undefined,
		[
			currentPart,
			playlist.continuousSource,
			playlist.isAudioOnly,
			usesContinuousSource,
		],
	);
	// Stable string identity of the loaded source (PlayerSrc is an opaque vidstack
	// union). Drives readiness tracking and source-swap detection.
	const currentSourceKey =
		usesContinuousSource && playlist.continuousSource
			? playlist.continuousSource.src
			: currentPart?.src;
	const isAudioSource = playlist.isAudioOnly;
	// The continuous (muxed) file plays on its own clock; this is its probed
	// duration when known, used to map player time onto the recording timeline.
	const continuousDurationSeconds = usesContinuousSource
		? (playlist.continuousSource?.durationSeconds ?? null)
		: null;

	const rememberCommittedSeek = useCallback(
		(seek: CommittedRecordingSeek) => {
			if (seek.canonicalSeconds >= playlist.totalDurationSeconds - 0.05) {
				committedSeekRef.current = null;
				return;
			}
			committedSeekRef.current = seek;
		},
		[playlist.totalDurationSeconds],
	);

	const seekToGlobal = useCallback(
		(offsetSeconds: number, options?: RecordingSeekOptions) => {
			const target = findPartForOffset(
				playlist.parts,
				offsetSeconds,
				playlist.totalDurationSeconds,
			);
			if (!target) return;
			const player = isAudioSource ? audioRef.current : playerRef.current;
			const resume = options?.resume ?? (player ? !player.paused : false);
			const mode = options?.mode ?? "commit";
			const trigger = options?.trigger;
			const sourceReady = readySourceKeyRef.current === currentSourceKey;
			const sourceKey = currentSourceKey ?? "";
			const targetSourceKey = usesContinuousSource
				? sourceKey
				: target.part.src;
			if (target.globalSeconds < playlist.totalDurationSeconds - 0.05) {
				handledEndedSourceRef.current = null;
			}
			setGlobalTime(target.globalSeconds);
			if (usesContinuousSource) {
				// In continuous mode the player runs on the muxed file's clock, which
				// can drift from the recording timeline — map the canonical offset onto
				// that clock before assigning currentTime.
				const playerSeconds = canonicalToPlayerTime(
					target.globalSeconds,
					playlist.totalDurationSeconds,
					continuousDurationSeconds,
				);
				pendingUserSeekRef.current = {
					canonicalSeconds: target.globalSeconds,
					expiresAt: Date.now() + 1500,
					sourceKey,
				};
				if (mode === "commit") {
					rememberCommittedSeek({
						canonicalSeconds: target.globalSeconds,
						sourceKey,
						sourceSeconds: playerSeconds,
					});
				}
				if (player && sourceReady) {
					pendingSeekRef.current = null;
					if (mode === "preview") {
						if (!isAudioSource) mediaRemote.seeking(playerSeconds, trigger);
					} else {
						player.currentTime = playerSeconds;
						if (resume && player.paused) {
							void playPlaybackController(player, trigger).catch(() => {});
						}
					}
				} else {
					// Media not ready (cold load / deep link); defer the player offset.
					if (mode === "commit") {
						pendingSeekRef.current = {
							localSeconds: playerSeconds,
							playbackRate: player?.playbackRate ?? 1,
							resume,
						};
					}
				}
				return;
			}
			pendingUserSeekRef.current = {
				canonicalSeconds: target.globalSeconds,
				expiresAt: Date.now() + 1500,
				sourceKey: targetSourceKey,
			};
			if (mode === "commit") {
				rememberCommittedSeek({
					canonicalSeconds: target.globalSeconds,
					sourceKey: targetSourceKey,
					sourceSeconds: target.localSeconds,
				});
			}
			if (target.part.position === partPosition && player) {
				if (sourceReady) {
					pendingSeekRef.current = null;
					if (mode === "preview") {
						if (!isAudioSource)
							mediaRemote.seeking(target.localSeconds, trigger);
					} else {
						player.currentTime = target.localSeconds;
						if (resume && player.paused) {
							void playPlaybackController(player, trigger).catch(() => {});
						}
					}
				} else {
					if (mode === "commit") {
						pendingSeekRef.current = {
							localSeconds: target.localSeconds,
							playbackRate: player.playbackRate ?? 1,
							resume,
						};
					}
				}
				return;
			}
			pendingSeekRef.current = {
				localSeconds: target.localSeconds,
				playbackRate: player?.playbackRate ?? 1,
				resume,
			};
			setPartPosition(target.part.position);
		},
		[
			continuousDurationSeconds,
			currentSourceKey,
			isAudioSource,
			mediaRemote,
			partPosition,
			playlist.parts,
			playlist.totalDurationSeconds,
			rememberCommittedSeek,
			usesContinuousSource,
		],
	);

	// biome-ignore lint/correctness/useExhaustiveDependencies: reset local playback state when the playlist identity changes.
	useEffect(() => {
		setForcePartSequencer(false);
		setPartPosition(0);
		setGlobalTime(0);
		setAudioMuted(false);
		setAudioPaused(true);
		setAudioPlaybackRate(1);
		setAudioVolume(1);
		pendingSeekRef.current = null;
		pendingUserSeekRef.current = null;
		committedSeekRef.current = null;
		wasPlayingRef.current = false;
		// Sync the mode tracker to the new recording so switching videos isn't
		// mistaken for a mid-session upgrade by the swap effect below.
		prevUsesContinuousRef.current = playlist.continuousSource != null;
	}, [playlist.videoId]);

	// Parts → single-file upgrade: when the lazily-built continuous source appears
	// mid-session, vidstack reloads with the new src; carry the current position
	// over (mapped onto the muxed clock) so playback resumes where it was instead
	// of jumping to the start. The reverse (continuous → parts on a media error)
	// is handled in handleError.
	useEffect(() => {
		const wasContinuous = prevUsesContinuousRef.current;
		prevUsesContinuousRef.current = usesContinuousSource;
		if (wasContinuous || !usesContinuousSource) return;
		pendingSeekRef.current = {
			localSeconds: canonicalToPlayerTime(
				globalTime,
				playlist.totalDurationSeconds,
				continuousDurationSeconds,
			),
			playbackRate:
				(isAudioSource ? audioRef.current : playerRef.current)?.playbackRate ??
				1,
			resume: wasPlayingRef.current,
		};
	}, [
		usesContinuousSource,
		globalTime,
		playlist.totalDurationSeconds,
		continuousDurationSeconds,
		isAudioSource,
	]);

	useEffect(() => {
		if (initialOffsetSeconds == null) return;
		const key = `${playlist.videoId}:${initialOffsetSeconds}`;
		if (appliedInitialSeekRef.current === key) return;
		appliedInitialSeekRef.current = key;
		seekToGlobal(initialOffsetSeconds, { resume: false });
	}, [initialOffsetSeconds, playlist.videoId, seekToGlobal]);

	const handleCanPlay = useCallback(() => {
		if (!currentSourceKey) return;
		// The source is now seekable; record its identity so seeks apply immediately.
		readySourceKeyRef.current = currentSourceKey;
		const pending = pendingSeekRef.current;
		const player = isAudioSource ? audioRef.current : playerRef.current;
		if (!pending || !player) return;
		pendingSeekRef.current = null;
		player.currentTime = pending.localSeconds;
		player.playbackRate = pending.playbackRate;
		if (pending.resume) void playPlaybackController(player).catch(() => {});
	}, [currentSourceKey, isAudioSource]);

	const handleEnded = useCallback(() => {
		const sourceKey = currentSourceKey ?? "";
		if (handledEndedSourceRef.current === sourceKey) return;
		handledEndedSourceRef.current = sourceKey;
		if (usesContinuousSource) {
			setGlobalTime(playlist.totalDurationSeconds);
			return;
		}
		if (!currentPart) return;
		const next = playlist.parts[currentPart.position + 1];
		if (!next) {
			setGlobalTime(playlist.totalDurationSeconds);
			return;
		}
		pendingSeekRef.current = {
			localSeconds: 0,
			playbackRate:
				(isAudioSource ? audioRef.current : playerRef.current)?.playbackRate ??
				1,
			resume: true,
		};
		setGlobalTime(next.startSeconds);
		setPartPosition(next.position);
	}, [
		currentPart,
		currentSourceKey,
		isAudioSource,
		playlist.parts,
		playlist.totalDurationSeconds,
		usesContinuousSource,
	]);

	const syncPlaybackTime = useCallback(
		(playerSeconds: number) => {
			if (!currentPart) return;
			const canonicalSeconds = usesContinuousSource
				? playerTimeToCanonical(
						playerSeconds,
						playlist.totalDurationSeconds,
						continuousDurationSeconds,
					)
				: globalTimeForPart(currentPart, playerSeconds);
			const pendingUserSeek = pendingUserSeekRef.current;
			if (pendingUserSeek?.sourceKey === (currentSourceKey ?? "")) {
				const diff = Math.abs(
					canonicalSeconds - pendingUserSeek.canonicalSeconds,
				);
				if (diff > 0.5 && Date.now() < pendingUserSeek.expiresAt) {
					return;
				}
				pendingUserSeekRef.current = null;
			}
			if (canonicalSeconds < playlist.totalDurationSeconds - 0.05) {
				handledEndedSourceRef.current = null;
			}
			const committedSeek = committedSeekRef.current;
			const player = isAudioSource ? audioRef.current : playerRef.current;
			if (
				committedSeek?.sourceKey === (currentSourceKey ?? "") &&
				player &&
				!player.paused &&
				canonicalSeconds > committedSeek.canonicalSeconds + 0.5
			) {
				committedSeekRef.current = null;
			}
			setGlobalTime(canonicalSeconds);
		},
		[
			continuousDurationSeconds,
			currentSourceKey,
			currentPart,
			isAudioSource,
			playlist.totalDurationSeconds,
			usesContinuousSource,
		],
	);

	const handlePausedChange = useCallback((paused: boolean) => {
		wasPlayingRef.current = !paused;
	}, []);

	const handleAudioTogglePlayback = useCallback(
		(event: MouseEvent<HTMLButtonElement>) => {
			const audio = audioRef.current;
			if (!audio) return;
			const trigger = event.nativeEvent;

			if (!audio.paused) {
				pausePlaybackController(audio, trigger);
				return;
			}

			const sourceKey = currentSourceKey ?? "";
			const replayingEndedSource =
				(handledEndedSourceRef.current === sourceKey || audio.ended) &&
				globalTime >= playlist.totalDurationSeconds - 0.05;
			if (replayingEndedSource) {
				committedSeekRef.current = null;
				seekToGlobal(0, { resume: true, trigger });
				return;
			}

			const committedSeek = committedSeekRef.current;
			if (committedSeek?.sourceKey === sourceKey) {
				handledEndedSourceRef.current = null;
				setGlobalTime(committedSeek.canonicalSeconds);
				audio.currentTime = committedSeek.sourceSeconds;
			} else {
				committedSeekRef.current = null;
			}

			void audio.play().catch(() => {});
		},
		[currentSourceKey, globalTime, playlist.totalDurationSeconds, seekToGlobal],
	);

	const handleAudioToggleMuted = useCallback(
		(event: MouseEvent<HTMLButtonElement>) => {
			const audio = audioRef.current;
			if (!audio) return;
			if (audio.muted || audio.volume <= 0) {
				if (audio.volume <= 0) audio.volume = 1;
				audio.muted = false;
			} else {
				audio.muted = true;
			}
			setAudioMuted(audio.muted);
			setAudioVolume(audio.volume);
			event.currentTarget.blur();
		},
		[],
	);

	const handleAudioVolumeChange = useCallback((volume: number) => {
		const audio = audioRef.current;
		if (!audio) return;
		audio.volume = volume;
		audio.muted = volume <= 0;
		setAudioMuted(audio.muted);
		setAudioVolume(audio.volume);
	}, []);

	const handleAudioPlaybackRateChange = useCallback((rate: number) => {
		const audio = audioRef.current;
		if (!audio) return;
		audio.playbackRate = rate;
		setAudioPlaybackRate(audio.playbackRate);
	}, []);

	if (!currentPart || !currentSource) return null;

	function handleError() {
		if (!usesContinuousSource || playlist.parts.length <= 1) return;
		const player = isAudioSource ? audioRef.current : playerRef.current;
		// player.currentTime is on the muxed clock; bring it back to the recording
		// timeline before locating the part to resume from.
		const canonicalSeconds =
			player?.currentTime != null
				? playerTimeToCanonical(
						player.currentTime,
						playlist.totalDurationSeconds,
						continuousDurationSeconds,
					)
				: globalTime;
		const target = findPartForOffset(
			playlist.parts,
			canonicalSeconds,
			playlist.totalDurationSeconds,
		);
		if (!target) return;
		pendingSeekRef.current = {
			localSeconds: target.localSeconds,
			playbackRate: player?.playbackRate ?? 1,
			resume: wasPlayingRef.current,
		};
		setGlobalTime(target.globalSeconds);
		setPartPosition(target.part.position);
		setForcePartSequencer(true);
	}

	function handleEventTimeUpdate(detail: { currentTime: number }) {
		if (handledEndedSourceRef.current === (currentSourceKey ?? "")) return;
		syncPlaybackTime(detail.currentTime);
	}

	function handlePlayerKeyDown(event: KeyboardEvent<HTMLElement>) {
		if (event.target !== event.currentTarget) return;
		const total = playlist.totalDurationSeconds;
		const next = seekKeyTarget(event.key, globalTime, total);
		if (next == null) return;
		event.preventDefault();
		event.stopPropagation();
		seekToGlobal(next);
	}

	const recordingTimeline = (
		<RecordingTimeline
			playlist={playlist}
			currentPart={currentPart}
			currentSeconds={globalTime}
			onSeek={seekToGlobal}
			showTimeLabels={!isAudioSource}
			waveformLoading={isAudioSource && audioWaveformLoading}
			waveformPeaks={isAudioSource ? audioWaveform?.peaks : null}
		/>
	);

	const mediaProvider = (
		<MediaProvider>
			{chapterCues.length > 0 && (
				<Track
					key={`${usesContinuousSource ? "recording" : currentPart.partIndex}:${chapterCues.map((cue) => `${cue.startTime}:${cue.text}`).join("|")}`}
					kind="chapters"
					label="Recording markers"
					type="json"
					content={chapterContent}
					default
				/>
			)}
		</MediaProvider>
	);

	if (isAudioSource) {
		return (
			<section
				aria-label={`Audio Player - ${playlist.title}`}
				className="rv-watch-player-audio rounded-xl shadow-sm"
			>
				{/* biome-ignore lint/a11y/useMediaCaption: Archived audio-only recordings do not have caption tracks. */}
				<audio
					ref={audioRef}
					src={currentSourceKey}
					crossOrigin={isCrossOrigin ? "use-credentials" : undefined}
					preload="metadata"
					className="hidden"
					onCanPlay={handleCanPlay}
					onEnded={handleEnded}
					onError={handleError}
					onLoadedMetadata={(event) => {
						setAudioMuted(event.currentTarget.muted);
						setAudioPaused(event.currentTarget.paused);
						setAudioPlaybackRate(event.currentTarget.playbackRate);
						setAudioVolume(event.currentTarget.volume);
					}}
					onPause={() => {
						wasPlayingRef.current = false;
						setAudioPaused(true);
					}}
					onPlay={() => {
						wasPlayingRef.current = true;
						setAudioPaused(false);
					}}
					onRateChange={(event) => {
						setAudioPlaybackRate(event.currentTarget.playbackRate);
					}}
					onTimeUpdate={(event) => {
						handleEventTimeUpdate({
							currentTime: event.currentTarget.currentTime,
						});
					}}
					onVolumeChange={(event) => {
						setAudioMuted(event.currentTarget.muted);
						setAudioVolume(event.currentTarget.volume);
					}}
				/>
				<AudioControls
					paused={audioPaused}
					muted={audioMuted}
					volume={audioVolume}
					canSetVolume
					currentSeconds={globalTime}
					playbackRate={audioPlaybackRate}
					totalSeconds={playlist.totalDurationSeconds}
					onTogglePlayback={handleAudioTogglePlayback}
					onToggleMuted={handleAudioToggleMuted}
					onVolumeChange={handleAudioVolumeChange}
					onPlaybackRateChange={handleAudioPlaybackRateChange}
					onSeekBackward={() => seekToGlobal(globalTime - 10)}
					onSeekForward={() => seekToGlobal(globalTime + 10)}
				/>
				<div className="rv-audio-recording-timeline">{recordingTimeline}</div>
			</section>
		);
	}

	return (
		<MediaPlayer
			ref={playerRef}
			src={currentSource}
			title={playlist.title}
			crossOrigin={isCrossOrigin ? "use-credentials" : null}
			playsInline
			onCanPlay={handleCanPlay}
			onEnded={handleEnded}
			onError={handleError}
			onKeyDown={handlePlayerKeyDown}
			onPause={() => {
				wasPlayingRef.current = false;
			}}
			onPlay={() => {
				wasPlayingRef.current = true;
			}}
			onTimeUpdate={handleEventTimeUpdate}
			className="rounded-lg overflow-hidden bg-black shadow-sm"
		>
			<MediaStateBridge
				onCanPlay={handleCanPlay}
				onEnded={handleEnded}
				onPausedChange={handlePausedChange}
				onTimeUpdate={syncPlaybackTime}
			/>
			{mediaProvider}
			<DefaultVideoLayout
				icons={defaultLayoutIcons}
				slots={{
					timeSlider: recordingTimeline,
				}}
			/>
		</MediaPlayer>
	);
}

type PlaybackController = HTMLMediaElement | MediaPlayerInstance;

function playPlaybackController(
	player: PlaybackController,
	trigger?: Event,
): Promise<void> {
	const play = player.play as (trigger?: Event) => Promise<void>;
	return play.call(player, trigger);
}

function pausePlaybackController(player: PlaybackController, trigger?: Event) {
	const pause = player.pause as (trigger?: Event) => Promise<void> | void;
	void pause.call(player, trigger);
}

function AudioControls({
	canSetVolume,
	currentSeconds,
	muted,
	onPlaybackRateChange,
	onSeekBackward,
	onSeekForward,
	onToggleMuted,
	onTogglePlayback,
	onVolumeChange,
	paused,
	playbackRate,
	totalSeconds,
	volume,
}: {
	canSetVolume: boolean;
	currentSeconds: number;
	muted: boolean;
	onPlaybackRateChange: (rate: number) => void;
	onTogglePlayback: (event: MouseEvent<HTMLButtonElement>) => void;
	onToggleMuted: (event: MouseEvent<HTMLButtonElement>) => void;
	onVolumeChange: (volume: number) => void;
	onSeekBackward: () => void;
	onSeekForward: () => void;
	paused: boolean;
	playbackRate: number;
	totalSeconds: number;
	volume: number;
}) {
	function cyclePlaybackRate(event: MouseEvent<HTMLButtonElement>) {
		const rates = [1, 1.25, 1.5, 1.75, 2];
		const currentIndex = rates.findIndex(
			(rate) => Math.abs(rate - playbackRate) < 0.01,
		);
		onPlaybackRateChange(rates[(currentIndex + 1) % rates.length] ?? 1);
		event.currentTarget.blur();
	}

	function changeVolume(event: ChangeEvent<HTMLInputElement>) {
		onVolumeChange(Number(event.currentTarget.value));
	}

	return (
		<div className="rv-audio-controls" data-testid="audio-controls">
			<div className="rv-audio-transport">
				<button
					type="button"
					className="rv-audio-icon-button"
					aria-label="Seek backward 10 seconds"
					title="Seek backward 10 seconds"
					onClick={onSeekBackward}
				>
					<RewindIcon className="size-6" weight="bold" />
				</button>
				<button
					type="button"
					className="rv-audio-play-button"
					aria-label={paused ? "Play" : "Pause"}
					title={paused ? "Play" : "Pause"}
					onClick={onTogglePlayback}
				>
					{paused ? (
						<PlayIcon className="size-6 translate-x-px" weight="fill" />
					) : (
						<PauseIcon className="size-6" weight="fill" />
					)}
				</button>
				<button
					type="button"
					className="rv-audio-icon-button"
					aria-label="Seek forward 10 seconds"
					title="Seek forward 10 seconds"
					onClick={onSeekForward}
				>
					<FastForwardIcon className="size-6" weight="bold" />
				</button>
				<span className="rv-audio-time-readout" aria-live="off">
					{formatPlaybackTime(currentSeconds)}
					<span aria-hidden="true"> / </span>
					{formatPlaybackTime(totalSeconds)}
				</span>
			</div>
			<div className="rv-audio-settings">
				<div className="rv-audio-volume">
					<button
						type="button"
						className="rv-audio-icon-button"
						aria-label={muted ? "Unmute" : "Mute"}
						title={muted ? "Unmute" : "Mute"}
						onClick={onToggleMuted}
					>
						{muted || volume <= 0 ? (
							<SpeakerSlashIcon className="size-6" weight="fill" />
						) : (
							<SpeakerHighIcon className="size-6" weight="fill" />
						)}
					</button>
					<input
						type="range"
						className="rv-audio-volume-slider"
						aria-label="Volume"
						min={0}
						max={1}
						step={0.01}
						value={muted ? 0 : volume}
						disabled={!canSetVolume}
						onChange={changeVolume}
						style={
							{
								"--rv-volume": `${(muted ? 0 : volume) * 100}%`,
							} as CSSProperties
						}
					/>
				</div>
				<button
					type="button"
					className="rv-audio-rate-button"
					aria-label="Change playback speed"
					title="Change playback speed"
					onClick={cyclePlaybackRate}
				>
					{formatPlaybackRateLabel(playbackRate)}
				</button>
			</div>
		</div>
	);
}

function MediaStateBridge({
	onCanPlay,
	onEnded,
	onPausedChange,
	onTimeUpdate,
}: {
	onCanPlay: () => void;
	onEnded: () => void;
	onPausedChange: (paused: boolean) => void;
	onTimeUpdate: (currentTime: number) => void;
}) {
	const canPlay = useMediaState("canPlay");
	const currentTime = useMediaState("currentTime");
	const ended = useMediaState("ended");
	const paused = useMediaState("paused");
	const previousEndedRef = useRef(false);

	useEffect(() => {
		if (canPlay) onCanPlay();
	}, [canPlay, onCanPlay]);

	useEffect(() => {
		onPausedChange(paused);
	}, [onPausedChange, paused]);

	useEffect(() => {
		if (!ended) onTimeUpdate(currentTime);
	}, [currentTime, ended, onTimeUpdate]);

	useEffect(() => {
		if (ended && !previousEndedRef.current) onEnded();
		previousEndedRef.current = ended;
	}, [ended, onEnded]);

	return null;
}

function RecordingTimeline({
	playlist,
	currentPart,
	currentSeconds,
	onSeek,
	showTimeLabels = true,
	waveformLoading,
	waveformPeaks,
}: {
	playlist: RecordingPlaylist;
	currentPart: RecordingPlaylistPart;
	currentSeconds: number;
	onSeek: (seconds: number, options?: RecordingSeekOptions) => void;
	showTimeLabels?: boolean;
	waveformLoading?: boolean;
	waveformPeaks?: number[] | null;
}) {
	const { t } = useTranslation();
	const total = playlist.totalDurationSeconds;
	const hasDuration = total > 0;
	const progress = percentOf(currentSeconds, total);
	const hasWaveform = !!waveformPeaks && waveformPeaks.length > 0;
	// The waveform lane is the audio scrubber's hero element, so it's tall.
	// Without a waveform (no peaks yet) it collapses to a thin progress bar.
	const showWaveformLane = hasWaveform || !!waveformLoading;
	const trackRef = useRef<HTMLDivElement>(null);
	const timelineSeek = useTimelinePointerSeek<HTMLDivElement>({
		totalSeconds: total,
		onSeek,
		getTrackElement: () => trackRef.current,
	});
	const partLabel = t("watch.part_status", {
		current: currentPart.position + 1,
		total: playlist.parts.length,
	});

	return (
		<div className="vds-recording-time-slider">
			{showTimeLabels && (
				<div className="mb-1 grid grid-cols-[auto_1fr_auto] items-center gap-2 text-[11px] tabular-nums text-white/75">
					<span className="min-w-10">{formatPlaybackTime(currentSeconds)}</span>
					<span className="truncate text-center text-[10px] uppercase tracking-wide text-white/60">
						{playlist.parts.length > 1 ? partLabel : ""}
					</span>
					<span className="min-w-10 text-right">
						{formatPlaybackTime(total)}
					</span>
				</div>
			)}
			<div className={cn("relative", showWaveformLane ? "h-24" : "h-5")}>
				<div
					role="slider"
					tabIndex={hasDuration ? 0 : -1}
					aria-disabled={!hasDuration}
					aria-valuemin={0}
					aria-valuemax={Math.round(total)}
					aria-valuenow={Math.round(
						Math.min(Math.max(currentSeconds, 0), total),
					)}
					aria-valuetext={`${formatPlaybackTime(currentSeconds)} / ${formatPlaybackTime(total)}`}
					className={cn(
						"absolute inset-x-0 -translate-y-1/2 cursor-pointer outline-none ring-offset-black transition focus-visible:ring-2 focus-visible:ring-primary focus-visible:ring-offset-2",
						showWaveformLane
							? "top-12 h-20 rounded-md"
							: "top-[13px] h-5 rounded-full",
						!hasDuration && "cursor-not-allowed opacity-50",
					)}
					aria-label={t("watch.seek_recording")}
					onPointerDown={timelineSeek.handlePointerDown}
					onPointerMove={timelineSeek.handlePointerMove}
					onPointerUp={timelineSeek.endPointerDrag}
					onPointerCancel={timelineSeek.endPointerDrag}
					onClick={timelineSeek.handleClick}
					onKeyDown={(event) => {
						const next = seekKeyTarget(event.key, currentSeconds, total);
						if (next == null) return;
						event.preventDefault();
						onSeek(next);
					}}
				>
					<div
						ref={trackRef}
						data-testid="recording-timeline-rail"
						className={cn(
							"pointer-events-none absolute inset-x-1.5 overflow-hidden",
							showWaveformLane
								? "inset-y-0 rounded-md bg-transparent"
								: "top-1/2 h-1.5 -translate-y-1/2 rounded-full bg-white/20",
						)}
					>
						{hasWaveform && (
							<TimelineWaveform peaks={waveformPeaks} progress={progress} />
						)}
						{!hasWaveform && waveformLoading && <TimelineWaveformLoading />}
						{!showWaveformLane && (
							<span
								className="block h-full rounded-full bg-primary"
								style={{ width: `${progress}%` }}
							/>
						)}
					</div>
				</div>
				<div className="pointer-events-none absolute inset-x-1.5 inset-y-0 overflow-visible">
					{hasDuration &&
						playlist.parts.length > 1 &&
						playlist.parts.map((part) => (
							<PartSegmentButton
								key={part.partIndex}
								part={part}
								totalSeconds={total}
								onSeek={onSeek}
								hasWaveform={showWaveformLane}
								getTrackElement={() => trackRef.current}
							/>
						))}
					{hasDuration &&
						playlist.parts
							.slice(1)
							.map((part) => (
								<PartBoundaryMarker
									key={part.partIndex}
									part={part}
									totalSeconds={total}
									hasWaveform={showWaveformLane}
								/>
							))}
					{hasDuration &&
						playlist.markers.map((marker) => (
							<MarkerButton
								key={marker.key}
								marker={marker}
								totalSeconds={total}
								onSeek={onSeek}
							/>
						))}
					{hasDuration && (
						<span
							aria-hidden="true"
							className={cn(
								"pointer-events-none absolute z-30 size-3 -translate-x-1/2 -translate-y-1/2 rounded-full border-2 border-black bg-primary shadow-sm",
								showWaveformLane ? "top-12" : "top-[13px]",
							)}
							data-testid="recording-progress-thumb"
							style={{ left: `${progress}%` }}
						/>
					)}
				</div>
			</div>
		</div>
	);
}

function TimelineWaveform({
	peaks,
	progress,
}: {
	peaks: number[];
	progress: number;
}) {
	const path = useMemo(() => waveformPath(peaks), [peaks]);
	const width = waveformViewBoxWidth(peaks);
	const viewBox = `0 0 ${width} 100`;
	// Both layers render the identical full-width waveform; only the
	// foreground is cropped to the played fraction. Clipping (rather than
	// resizing the progress box) keeps every peak aligned with the grey
	// track behind it.
	const clipRight = 100 - clamp(progress, 0, 100);

	return (
		<div
			className="rv-recording-waveform"
			data-testid="audio-waveform"
			aria-hidden="true"
		>
			<svg viewBox={viewBox} preserveAspectRatio="none" aria-hidden="true">
				<path d={path} />
			</svg>
			<div
				className="rv-recording-waveform-progress"
				style={{ clipPath: `inset(0 ${clipRight}% 0 0)` }}
			>
				<svg viewBox={viewBox} preserveAspectRatio="none" aria-hidden="true">
					<path d={path} />
				</svg>
			</div>
		</div>
	);
}

function TimelineWaveformLoading() {
	const peaks = useMemo(
		() =>
			Array.from({ length: 96 }, (_, index) => {
				const wave = Math.sin(index * 0.55) * 0.5 + 0.5;
				const pulse = Math.sin(index * 0.17 + 1.2) * 0.5 + 0.5;
				return 0.08 + wave * 0.34 + pulse * 0.16;
			}),
		[],
	);
	const path = useMemo(() => waveformPath(peaks), [peaks]);
	const width = waveformViewBoxWidth(peaks);

	return (
		<div
			className="rv-recording-waveform rv-recording-waveform-loading"
			aria-hidden="true"
		>
			<svg
				viewBox={`0 0 ${width} 100`}
				preserveAspectRatio="none"
				aria-hidden="true"
			>
				<path d={path} />
			</svg>
		</div>
	);
}

function PartSegmentButton({
	getTrackElement,
	hasWaveform,
	part,
	totalSeconds,
	onSeek,
}: {
	getTrackElement: () => HTMLElement | null;
	hasWaveform: boolean;
	part: RecordingPlaylistPart;
	totalSeconds: number;
	onSeek: (seconds: number, options?: RecordingSeekOptions) => void;
}) {
	const { t } = useTranslation();
	const left = percentOf(part.startSeconds, totalSeconds);
	const width = percentOf(part.durationSeconds, totalSeconds);
	const start = formatPlaybackTime(part.startSeconds);
	const end = formatPlaybackTime(part.endSeconds);
	const align = popoverAlign(left + width / 2);
	const timelineSeek = useTimelinePointerSeek<HTMLButtonElement>({
		totalSeconds,
		onSeek,
		getTrackElement,
	});

	// Track the pointer's horizontal offset within the segment as a CSS var so the
	// hover popover follows the cursor along the part (set imperatively to avoid a
	// re-render per move). Before the first move the var is unset and the popover
	// falls back to centered.
	const handlePointerMove = (e: PointerEvent<HTMLButtonElement>) => {
		const el = e.currentTarget;
		const rect = el.getBoundingClientRect();
		const x = Math.min(rect.width, Math.max(0, e.clientX - rect.left));
		el.style.setProperty("--seg-cursor", `${x}px`);
		timelineSeek.handlePointerMove(e);
	};

	return (
		<button
			type="button"
			className={cn(
				"pointer-events-auto group absolute z-10 min-w-2 -translate-y-1/2 overflow-visible rounded-sm border-0 bg-transparent p-0 outline-none ring-offset-black transition hover:bg-white/10 focus-visible:ring-2 focus-visible:ring-primary focus-visible:ring-offset-2",
				hasWaveform ? "top-12 h-20" : "top-[13px] h-5",
				align === "start" && "translate-x-0",
			)}
			style={{
				left: `${left}%`,
				width: `${Math.max(0.5, width)}%`,
			}}
			aria-label={t("watch.part_segment", {
				part: part.position + 1,
				start,
				end,
			})}
			onPointerDown={timelineSeek.handlePointerDown}
			onPointerMove={handlePointerMove}
			onPointerUp={timelineSeek.endPointerDrag}
			onPointerCancel={timelineSeek.endPointerDrag}
			onClick={timelineSeek.handleClick}
		>
			<span
				className="pointer-events-none absolute z-50 hidden w-max max-w-[min(22rem,calc(100vw-4rem))] -translate-x-1/2 rounded-md border border-white/15 bg-black/90 px-3 py-2 text-left text-xs leading-snug text-white shadow-xl ring-1 ring-white/10 backdrop-blur group-hover:block group-focus-visible:block"
				style={{
					bottom: "calc(100% + 0.5rem)",
					left: "var(--seg-cursor, 50%)",
				}}
			>
				<PartSegmentPopoverContent part={part} start={start} end={end} />
			</span>
		</button>
	);
}

function PartBoundaryMarker({
	hasWaveform,
	part,
	totalSeconds,
}: {
	hasWaveform: boolean;
	part: RecordingPlaylistPart;
	totalSeconds: number;
}) {
	const left = percentOf(part.startSeconds, totalSeconds);
	return (
		<span
			aria-hidden="true"
			className={cn(
				"pointer-events-none absolute z-20 w-px -translate-y-1/2 bg-black/70 shadow-[0_0_0_1px_rgb(255_255_255/0.22)]",
				hasWaveform ? "top-12 h-8" : "top-[13px] h-3",
			)}
			style={{ left: `${left}%` }}
		/>
	);
}

function PartSegmentPopoverContent({
	part,
	start,
	end,
}: {
	part: RecordingPlaylistPart;
	start: string;
	end: string;
}) {
	const { t } = useTranslation();
	const meta = [
		t("watch.part_duration", {
			duration: formatPlaybackTime(part.durationSeconds),
		}),
		t("watch.part_size", { size: formatBytes(part.sizeBytes) }),
	].join(" · ");
	return (
		<TimelinePartContent
			tone="video"
			heading={t("watch.part_label", { part: part.position + 1 })}
			range={t("watch.part_range", { start, end })}
			meta={meta}
		/>
	);
}

function MarkerButton({
	marker,
	totalSeconds,
	onSeek,
}: {
	marker: RecordingTimelineMarker;
	totalSeconds: number;
	onSeek: (seconds: number, options?: RecordingSeekOptions) => void;
}) {
	const { t } = useTranslation();
	const left = percentOf(marker.offsetSeconds, totalSeconds);
	const time = formatPlaybackTime(marker.offsetSeconds);
	const align = popoverAlign(left);
	return (
		<button
			type="button"
			className={cn(
				"pointer-events-auto group absolute top-0 z-20 h-2.5 w-1.5 overflow-visible rounded-full border border-black/80 bg-white shadow-sm outline-none ring-offset-black transition hover:h-3.5 hover:w-2 hover:-translate-y-0.5 focus-visible:ring-2 focus-visible:ring-primary focus-visible:ring-offset-2",
				align === "start" && "translate-x-0",
				align === "center" && "-translate-x-1/2",
				align === "end" && "-translate-x-full",
			)}
			style={{ left: `${left}%` }}
			aria-label={t("watch.seek_marker", {
				label: marker.label,
				time,
			})}
			onClick={(event) =>
				onSeek(marker.offsetSeconds, { trigger: event.nativeEvent })
			}
		>
			<span
				className={cn(
					"pointer-events-none absolute z-50 hidden w-max max-w-[min(24rem,calc(100vw-4rem))] rounded-md border border-white/15 bg-black/90 px-3 py-2 text-left text-xs leading-snug text-white shadow-xl ring-1 ring-white/10 backdrop-blur group-hover:block group-focus-visible:block",
					align === "start" && "left-0",
					align === "center" && "left-1/2 -translate-x-1/2",
					align === "end" && "right-0",
				)}
				style={{ bottom: "calc(100% + 0.5rem)" }}
			>
				<MarkerPopoverContent marker={marker} time={time} />
			</span>
		</button>
	);
}

function MarkerPopoverContent({
	marker,
	time,
}: {
	marker: RecordingTimelineMarker;
	time: string;
}) {
	const title = marker.event.title?.name.trim();
	const category = marker.event.category?.name.trim();
	return (
		<TimelineChangeContent
			tone="video"
			change={{
				time,
				category:
					marker.changes.includes("category") && category
						? { name: category, boxArtUrl: marker.event.category?.box_art_url }
						: undefined,
				title:
					marker.changes.includes("title") && title
						? { name: title }
						: undefined,
				fallback: marker.label,
			}}
		/>
	);
}

function useTimelinePointerSeek<T extends HTMLElement>({
	totalSeconds,
	onSeek,
	getTrackElement,
}: {
	totalSeconds: number;
	onSeek: (seconds: number, options?: RecordingSeekOptions) => void;
	getTrackElement?: (element: T) => HTMLElement | null;
}) {
	const draggingRef = useRef(false);
	const suppressClickRef = useRef(false);
	const seekFromEvent = useCallback(
		(
			event: MouseEvent<T> | PointerEvent<T>,
			mode: RecordingSeekMode = "commit",
		) => {
			const trackElement = (getTrackElement ?? selfElement)(
				event.currentTarget,
			);
			if (!trackElement) return;
			const seconds = secondsFromClientX(
				event.clientX,
				trackElement,
				totalSeconds,
			);
			if (seconds == null) return;
			onSeek(seconds, { mode, trigger: event.nativeEvent });
		},
		[getTrackElement, onSeek, totalSeconds],
	);
	const handleClick = useCallback(
		(event: MouseEvent<T>) => {
			if (suppressClickRef.current) {
				suppressClickRef.current = false;
				return;
			}
			seekFromEvent(event);
		},
		[seekFromEvent],
	);
	const handlePointerDown = useCallback(
		(event: PointerEvent<T>) => {
			if (event.button !== 0) return;
			draggingRef.current = true;
			suppressClickRef.current = true;
			event.currentTarget.setPointerCapture?.(event.pointerId);
			seekFromEvent(event, "preview");
		},
		[seekFromEvent],
	);
	const handlePointerMove = useCallback(
		(event: PointerEvent<T>) => {
			if (!draggingRef.current) return;
			seekFromEvent(event, "preview");
		},
		[seekFromEvent],
	);
	const endPointerDrag = useCallback(
		(event: PointerEvent<T>) => {
			if (!draggingRef.current) return;
			draggingRef.current = false;
			event.currentTarget.releasePointerCapture?.(event.pointerId);
			seekFromEvent(event, "commit");
		},
		[seekFromEvent],
	);

	return {
		handleClick,
		handlePointerDown,
		handlePointerMove,
		endPointerDrag,
	};
}

function secondsFromClientX(
	clientX: number,
	trackElement: HTMLElement,
	totalSeconds: number,
): number | null {
	if (totalSeconds <= 0) return null;
	const rect = trackElement.getBoundingClientRect();
	const ratio =
		rect.width <= 0 ? 0 : clamp((clientX - rect.left) / rect.width, 0, 1);
	return ratio * totalSeconds;
}

function seekKeyTarget(
	key: string,
	currentSeconds: number,
	totalSeconds: number,
): number | null {
	if (totalSeconds <= 0) return null;
	switch (key) {
		case "ArrowLeft":
			return clamp(currentSeconds - 10, 0, totalSeconds);
		case "ArrowRight":
			return clamp(currentSeconds + 10, 0, totalSeconds);
		case "PageDown":
			return clamp(currentSeconds - 60, 0, totalSeconds);
		case "PageUp":
			return clamp(currentSeconds + 60, 0, totalSeconds);
		case "Home":
			return 0;
		case "End":
			return totalSeconds;
		default:
			return null;
	}
}

function formatPlaybackRateLabel(playbackRate: number): string {
	return `${Number.isInteger(playbackRate) ? playbackRate : playbackRate.toFixed(2).replace(/0$/, "")}x`;
}

function waveformPath(peaks: number[]): string {
	if (peaks.length === 0) return "";
	const width = waveformViewBoxWidth(peaks);
	return peaks
		.map((peak, index) => {
			const amplitude = clamp(peak, 0, 1);
			const halfHeight = 2 + amplitude * 46;
			const x = peaks.length === 1 ? width / 2 : index;
			return `M ${x} ${50 - halfHeight} V ${50 + halfHeight}`;
		})
		.join(" ");
}

function waveformViewBoxWidth(peaks: number[]): number {
	return Math.max(1, peaks.length - 1);
}

function selfElement<T extends HTMLElement>(element: T): HTMLElement {
	return element;
}

// The continuous (muxed) file can probe to a slightly different duration than the
// recording timeline (summed part EXTINF). These map between the canonical
// recording seconds the UI renders and the player's own clock, isolating that
// drift to the player I/O boundary. A null or non-positive probed duration means
// "no drift" — pass the value through unchanged.
function canonicalToPlayerTime(
	canonicalSeconds: number,
	totalSeconds: number,
	continuousDurationSeconds: number | null,
): number {
	if (
		continuousDurationSeconds == null ||
		continuousDurationSeconds <= 0 ||
		totalSeconds <= 0
	) {
		return canonicalSeconds;
	}
	return canonicalSeconds * (continuousDurationSeconds / totalSeconds);
}

function playerTimeToCanonical(
	playerSeconds: number,
	totalSeconds: number,
	continuousDurationSeconds: number | null,
): number {
	if (
		continuousDurationSeconds == null ||
		continuousDurationSeconds <= 0 ||
		totalSeconds <= 0
	) {
		return playerSeconds;
	}
	return playerSeconds * (totalSeconds / continuousDurationSeconds);
}
