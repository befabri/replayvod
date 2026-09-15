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
	type ReactNode,
	useCallback,
	useEffect,
	useMemo,
	useRef,
	useState,
} from "react";
import { useTranslation } from "react-i18next";
import { API_URL } from "@/env";
import {
	type MediaFailureKind,
	MediaUnavailablePanel,
} from "@/features/videos/components/MediaUnavailablePanel";
import { ResumeNotice } from "@/features/videos/components/ResumeNotice";
import {
	TimelineChangeContent,
	TimelinePartContent,
} from "@/features/videos/components/timelinePopover";
import { formatBytes, formatPlaybackTime } from "@/features/videos/format";
import { useFullscreenOrientation } from "@/features/videos/fullscreen-orientation";
import {
	chapterCuesForPart,
	chapterCuesForRecording,
	findPartForOffset,
	globalTimeForPart,
	probeMediaSource,
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
	initial?: boolean;
};

type CommittedRecordingSeek = {
	canonicalSeconds: number;
	sourceKey: string;
	sourceSeconds: number;
};

const WATCH_PROGRESS_SAVE_INTERVAL_MS = 15_000;
const WATCH_PROGRESS_SAVE_DELTA_SECONDS = 15;
const SEEK_READBACK_TOLERANCE_SECONDS = 0.5;
const RESUME_NOTICE_MS = 8_000;
const MEDIA_LOAD_WATCHDOG_MS = 20_000;

export function WatchPlayer({
	audioWaveform,
	audioWaveformLoading,
	thumbnailUrl,
	playlist,
	initialOffsetSeconds,
	resumedFromSeconds,
	onProgress,
	onMediaUnavailable,
	unavailableActions,
}: {
	audioWaveform?: { peaks: number[] } | null;
	audioWaveformLoading?: boolean;
	thumbnailUrl?: string | null;
	playlist: RecordingPlaylist;
	initialOffsetSeconds?: number;
	resumedFromSeconds?: number;
	onProgress?: (positionSeconds: number, completed: boolean) => void;
	onMediaUnavailable?: (kind: "gone" | "removed") => void;
	unavailableActions?: ReactNode;
}) {
	const isCrossOrigin = !!API_URL;
	const [isFullscreen, setIsFullscreen] = useState(false);
	const fullscreenOrientation = useFullscreenOrientation(isFullscreen);
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
	const lastProgressEmitRef = useRef<{
		at: number;
		positionSeconds: number;
	} | null>(null);

	const committedSeekRef = useRef<CommittedRecordingSeek | null>(null);
	const wasPlayingRef = useRef(false);
	const viewerEngagedRef = useRef(false);
	const globalTimeRef = useRef(0);
	const mediaRemote = useMediaRemote(playerRef);
	const prevUsesContinuousRef = useRef(playlist.continuousSource != null);
	const readySourceKeyRef = useRef<string | undefined>(undefined);
	const [partPosition, setPartPosition] = useState(0);
	const [globalTime, setGlobalTime] = useState(0);
	const [forcePartSequencer, setForcePartSequencer] = useState(false);
	const [mediaFailure, setMediaFailure] = useState<MediaFailureKind | null>(
		null,
	);
	const [reloadNonce, setReloadNonce] = useState(0);
	const [audioMuted, setAudioMuted] = useState(false);
	const [audioPaused, setAudioPaused] = useState(true);
	const [audioPlaybackRate, setAudioPlaybackRate] = useState(1);
	const [audioVolume, setAudioVolume] = useState(1);
	const [resumeNoticeDismissed, setResumeNoticeDismissed] = useState(false);

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
	const currentSourceKey =
		usesContinuousSource && playlist.continuousSource
			? playlist.continuousSource.src
			: currentPart?.src;
	const isAudioSource = playlist.isAudioOnly;
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
			if (!options?.initial) viewerEngagedRef.current = true;
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
		setMediaFailure(null);
		setPartPosition(0);
		setGlobalTime(0);
		setAudioMuted(false);
		setAudioPaused(true);
		setAudioPlaybackRate(1);
		setAudioVolume(1);
		pendingSeekRef.current = null;
		appliedInitialSeekRef.current = null;
		pendingUserSeekRef.current = null;
		committedSeekRef.current = null;
		lastProgressEmitRef.current = null;
		wasPlayingRef.current = false;
		viewerEngagedRef.current = false;
		setResumeNoticeDismissed(false);
		prevUsesContinuousRef.current = playlist.continuousSource != null;
	}, [playlist.videoId]);

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
		seekToGlobal(initialOffsetSeconds, { resume: false, initial: true });
	}, [initialOffsetSeconds, playlist.videoId, seekToGlobal]);

	const handleCanPlay = useCallback(() => {
		if (!currentSourceKey) return;
		readySourceKeyRef.current = currentSourceKey;
		const pending = pendingSeekRef.current;
		const player = isAudioSource ? audioRef.current : playerRef.current;
		if (!pending || !player) return;
		player.currentTime = pending.localSeconds;
		if (
			isAudioSource &&
			Math.abs(player.currentTime - pending.localSeconds) >
				SEEK_READBACK_TOLERANCE_SECONDS
		) {
			return;
		}
		pendingSeekRef.current = null;
		player.playbackRate = pending.playbackRate;
		if (pending.resume) void playPlaybackController(player).catch(() => {});
	}, [currentSourceKey, isAudioSource]);

	const emitWatchProgress = useCallback(
		(positionSeconds: number, completed = false, force = false) => {
			if (!onProgress) return;
			if (!completed && !viewerEngagedRef.current) return;
			const total = playlist.totalDurationSeconds;
			if (!Number.isFinite(positionSeconds)) return;
			const clamped =
				total > 0
					? clamp(positionSeconds, 0, total)
					: Math.max(0, positionSeconds);
			if (!completed && clamped < 1 && !force) return;
			const last = lastProgressEmitRef.current;
			const now = Date.now();
			if (
				!force &&
				last &&
				now - last.at < WATCH_PROGRESS_SAVE_INTERVAL_MS &&
				Math.abs(clamped - last.positionSeconds) <
					WATCH_PROGRESS_SAVE_DELTA_SECONDS
			) {
				return;
			}
			if (force && !completed && last?.positionSeconds === clamped) return;
			lastProgressEmitRef.current = { at: now, positionSeconds: clamped };
			onProgress(clamped, completed);
		},
		[onProgress, playlist.totalDurationSeconds],
	);

	useEffect(() => {
		globalTimeRef.current = globalTime;
	}, [globalTime]);

	const readLiveGlobalTime = useCallback(() => {
		const player = isAudioSource ? audioRef.current : playerRef.current;
		if (
			!player ||
			!currentPart ||
			readySourceKeyRef.current !== currentSourceKey ||
			!Number.isFinite(player.currentTime)
		) {
			return globalTimeRef.current;
		}
		return usesContinuousSource
			? playerTimeToCanonical(
					player.currentTime,
					playlist.totalDurationSeconds,
					continuousDurationSeconds,
				)
			: globalTimeForPart(currentPart, player.currentTime);
	}, [
		continuousDurationSeconds,
		currentPart,
		currentSourceKey,
		isAudioSource,
		playlist.totalDurationSeconds,
		usesContinuousSource,
	]);

	const flushWatchProgressRef = useRef(() => {});
	useEffect(() => {
		flushWatchProgressRef.current = () =>
			emitWatchProgress(readLiveGlobalTime(), false, true);
	}, [emitWatchProgress, readLiveGlobalTime]);

	useEffect(() => {
		const flush = () => flushWatchProgressRef.current();
		const handleVisibilityChange = () => {
			if (document.visibilityState === "hidden") flush();
		};
		document.addEventListener("visibilitychange", handleVisibilityChange);
		window.addEventListener("pagehide", flush);
		return () => {
			document.removeEventListener("visibilitychange", handleVisibilityChange);
			window.removeEventListener("pagehide", flush);
			flush();
		};
	}, []);

	const showResumeNotice = resumedFromSeconds != null && !resumeNoticeDismissed;
	useEffect(() => {
		if (!showResumeNotice) return;
		const timer = window.setTimeout(
			() => setResumeNoticeDismissed(true),
			RESUME_NOTICE_MS,
		);
		return () => window.clearTimeout(timer);
	}, [showResumeNotice]);
	const startOver = useCallback(() => {
		setResumeNoticeDismissed(true);
		seekToGlobal(0);
	}, [seekToGlobal]);
	const resumeNotice =
		showResumeNotice && resumedFromSeconds != null ? (
			<div className="pointer-events-none absolute inset-x-0 top-full z-10 mt-2 motion-safe:animate-in motion-safe:fade-in-0 motion-safe:slide-in-from-top-2 motion-safe:duration-300">
				<ResumeNotice
					offsetSeconds={resumedFromSeconds}
					onStartOver={startOver}
					onDismiss={() => setResumeNoticeDismissed(true)}
					className="shadow-lg"
				/>
			</div>
		) : null;

	const handleEnded = useCallback(() => {
		const sourceKey = currentSourceKey ?? "";
		if (handledEndedSourceRef.current === sourceKey) return;
		handledEndedSourceRef.current = sourceKey;
		if (usesContinuousSource) {
			setGlobalTime(playlist.totalDurationSeconds);
			emitWatchProgress(playlist.totalDurationSeconds, true, true);
			return;
		}
		if (!currentPart) return;
		const next = playlist.parts[currentPart.position + 1];
		if (!next) {
			setGlobalTime(playlist.totalDurationSeconds);
			emitWatchProgress(playlist.totalDurationSeconds, true, true);
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
		emitWatchProgress,
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
			if (player && !player.paused) {
				emitWatchProgress(canonicalSeconds);
			}
		},
		[
			continuousDurationSeconds,
			currentSourceKey,
			currentPart,
			emitWatchProgress,
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

	const probeRef = useRef<AbortController | null>(null);
	const probeGeneration = useRef(0);
	const unavailableCallback = useRef(onMediaUnavailable);
	useEffect(() => {
		unavailableCallback.current = onMediaUnavailable;
	}, [onMediaUnavailable]);
	// biome-ignore lint/correctness/useExhaustiveDependencies: A source change or retry must cancel the previous probe.
	useEffect(() => {
		probeGeneration.current++;
		probeRef.current?.abort();
		probeRef.current = null;
		setMediaFailure(null);
		return () => {
			probeGeneration.current++;
			probeRef.current?.abort();
			probeRef.current = null;
		};
	}, [currentSourceKey, reloadNonce]);
	const reportMediaFailure = useCallback(() => {
		const src = currentSourceKey;
		if (!src || probeRef.current) return;
		const controller = new AbortController();
		probeRef.current = controller;
		const generation = probeGeneration.current;
		void probeMediaSource(src, fetch, controller.signal).then((result) => {
			if (controller.signal.aborted || generation !== probeGeneration.current)
				return;
			probeRef.current = null;
			const kind = result === "ok" ? "failed" : result;
			setMediaFailure(kind);
			if (kind !== "failed") unavailableCallback.current?.(kind);
		});
	}, [currentSourceKey]);
	// biome-ignore lint/correctness/useExhaustiveDependencies: Retrying the same source must start a fresh watchdog.
	useEffect(() => {
		const src = currentSourceKey;
		if (!src || mediaFailure) return;
		const timer = window.setTimeout(() => {
			if (readySourceKeyRef.current !== src) reportMediaFailure();
		}, MEDIA_LOAD_WATCHDOG_MS);
		return () => window.clearTimeout(timer);
	}, [currentSourceKey, mediaFailure, reloadNonce, reportMediaFailure]);

	const retryMedia = useCallback(() => {
		probeGeneration.current++;
		probeRef.current?.abort();
		probeRef.current = null;
		pendingSeekRef.current = {
			localSeconds: usesContinuousSource
				? canonicalToPlayerTime(
						globalTime,
						playlist.totalDurationSeconds,
						continuousDurationSeconds,
					)
				: Math.max(0, globalTime - (currentPart?.startSeconds ?? 0)),
			playbackRate: 1,
			resume: false,
		};
		readySourceKeyRef.current = undefined;
		setMediaFailure(null);
		setIsFullscreen(false);
		setReloadNonce((nonce) => nonce + 1);
	}, [
		continuousDurationSeconds,
		currentPart,
		globalTime,
		playlist.totalDurationSeconds,
		usesContinuousSource,
	]);

	if (!currentPart || !currentSource) {
		return (
			<MediaUnavailablePanel
				kind="gone"
				onRetry={() => onMediaUnavailable?.("gone")}
				actions={unavailableActions}
				compact={playlist.isAudioOnly}
			/>
		);
	}

	function handleError() {
		if (!usesContinuousSource || playlist.parts.length <= 1) {
			reportMediaFailure();
			return;
		}
		const player = isAudioSource ? audioRef.current : playerRef.current;
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
			popoverSide={isAudioSource ? "bottom" : "top"}
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

	if (mediaFailure) {
		return (
			<MediaUnavailablePanel
				kind={mediaFailure}
				onRetry={retryMedia}
				actions={unavailableActions}
				compact={isAudioSource}
			/>
		);
	}

	if (isAudioSource) {
		return (
			<section
				aria-label={`Audio Player - ${playlist.title}`}
				className="rv-watch-player-audio @container relative z-20 flex aspect-auto h-auto min-h-0 w-full flex-col items-stretch overflow-visible rounded-xl border border-border bg-card text-card-foreground shadow-sm"
			>
				{/* biome-ignore lint/a11y/useMediaCaption: Archived audio-only recordings do not have caption tracks. */}
				<audio
					key={reloadNonce}
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
						handleCanPlay();
					}}
					onLoadedData={handleCanPlay}
					onPlaying={handleCanPlay}
					onPause={() => {
						wasPlayingRef.current = false;
						setAudioPaused(true);
						flushWatchProgressRef.current();
					}}
					onPlay={() => {
						wasPlayingRef.current = true;
						viewerEngagedRef.current = true;
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
				<div className="flex w-full min-w-0 items-stretch gap-4 px-4 pt-3 pb-[1.1rem] @max-[40rem]:px-3 @max-[40rem]:pt-[0.9rem]">
					{thumbnailUrl ? <AudioThumbnail src={thumbnailUrl} /> : null}
					<div
						className="flex min-w-0 flex-auto flex-col"
						data-testid="audio-stack"
					>
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
						<div className="mt-auto w-full min-w-0 pt-2">
							{recordingTimeline}
						</div>
					</div>
				</div>
				{resumeNotice}
			</section>
		);
	}

	return (
		<div className="relative flex flex-col gap-2">
			<MediaPlayer
				key={reloadNonce}
				ref={playerRef}
				src={currentSource}
				title={playlist.title}
				crossOrigin={isCrossOrigin ? "use-credentials" : null}
				fullscreenOrientation={fullscreenOrientation}
				playsInline
				onCanPlay={handleCanPlay}
				onEnded={handleEnded}
				onError={handleError}
				onFullscreenChange={(active) => setIsFullscreen(active)}
				onKeyDown={handlePlayerKeyDown}
				onPause={() => {
					wasPlayingRef.current = false;
					flushWatchProgressRef.current();
				}}
				onPlay={() => {
					wasPlayingRef.current = true;
					viewerEngagedRef.current = true;
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
			{resumeNotice}
		</div>
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

function AudioThumbnail({ src }: { src: string }) {
	const [failedSrc, setFailedSrc] = useState<string | null>(null);
	if (src === failedSrc) return null;

	return (
		<div
			className="rv-audio-thumbnail relative hidden aspect-video flex-none self-center overflow-hidden rounded-[0.75rem] bg-[rgb(255_255_255/0.06)] @min-[50rem]:block"
			data-testid="audio-thumbnail"
		>
			<img
				src={src}
				alt=""
				className="absolute inset-0 h-full w-full object-cover"
				onError={() => setFailedSrc(src)}
			/>
		</div>
	);
}

const AUDIO_BUTTON_BASE =
	"inline-flex h-10 flex-none items-center justify-center border-0 outline-none transition-[background-color,color,opacity] duration-150 ease-[ease] focus-visible:shadow-[0_0_0_2px_rgb(0_0_0/0.9),0_0_0_4px_var(--primary)]";
const AUDIO_ICON_BUTTON = `${AUDIO_BUTTON_BASE} w-10 rounded-[0.5rem] bg-transparent text-inherit hover:bg-[rgb(255_255_255/0.12)]`;
const AUDIO_PLAY_BUTTON = `${AUDIO_BUTTON_BASE} w-10 rounded-full bg-primary text-primary-foreground hover:opacity-90`;
const AUDIO_RATE_BUTTON = `${AUDIO_BUTTON_BASE} min-w-[3.25rem] rounded-[0.5rem] bg-transparent px-[0.65rem] text-[0.78rem] font-bold text-inherit tabular-nums hover:bg-[rgb(255_255_255/0.12)] @max-[40rem]:min-w-[2.85rem] @max-[40rem]:px-[0.45rem]`;

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
		<div
			className="flex w-full min-w-0 items-center justify-between gap-4 pb-[0.4rem] @max-[40rem]:grid @max-[40rem]:grid-cols-[minmax(0,1fr)] @max-[40rem]:justify-items-center @max-[40rem]:gap-[0.65rem] @max-[40rem]:pb-[0.35rem]"
			data-testid="audio-controls"
		>
			<div className="flex min-w-0 items-center gap-[0.55rem] @max-[40rem]:flex-wrap @max-[40rem]:justify-center @max-[40rem]:gap-3">
				<button
					type="button"
					className={AUDIO_ICON_BUTTON}
					aria-label="Seek backward 10 seconds"
					title="Seek backward 10 seconds"
					onClick={onSeekBackward}
				>
					<RewindIcon className="size-6" weight="bold" />
				</button>
				<button
					type="button"
					className={AUDIO_PLAY_BUTTON}
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
					className={AUDIO_ICON_BUTTON}
					aria-label="Seek forward 10 seconds"
					title="Seek forward 10 seconds"
					onClick={onSeekForward}
				>
					<FastForwardIcon className="size-6" weight="bold" />
				</button>
				<span
					className="ms-[0.8rem] min-w-48 flex-none ps-1 text-[clamp(1.15rem,2.875cqw,1.45rem)] leading-none font-medium whitespace-nowrap text-card-foreground tabular-nums @max-[40rem]:ms-0 @max-[40rem]:min-w-full @max-[40rem]:ps-0 @max-[40rem]:text-center @max-[40rem]:text-[1.1rem]"
					aria-live="off"
					data-testid="audio-time-readout"
				>
					{formatPlaybackTime(currentSeconds)}
					<span aria-hidden="true"> / </span>
					{formatPlaybackTime(totalSeconds)}
				</span>
			</div>
			<div className="flex min-w-0 items-center justify-end gap-[0.55rem] @max-[40rem]:w-full @max-[40rem]:justify-center">
				<div className="flex min-w-0 items-center gap-[0.35rem] @max-[40rem]:max-w-48 @max-[40rem]:flex-1 @max-[40rem]:justify-center">
					<button
						type="button"
						className={AUDIO_ICON_BUTTON}
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
						className="rv-audio-volume-slider h-4 w-[clamp(5.5rem,14cqw,8rem)] cursor-pointer appearance-none border-0 bg-transparent outline-none focus-visible:shadow-[0_0_0_2px_rgb(0_0_0/0.9),0_0_0_4px_var(--primary)] disabled:cursor-not-allowed disabled:opacity-45 @max-[40rem]:w-auto @max-[40rem]:max-w-[8.5rem] @max-[40rem]:min-w-18 @max-[40rem]:flex-auto"
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
					className={AUDIO_RATE_BUTTON}
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

type TimelinePopoverSide = "top" | "bottom";

const TIMELINE_LANE = {
	waveform: { heightClass: "h-24", belowLane: "6.5rem" },
	compact: { heightClass: "h-5", belowLane: "1.75rem" },
} as const;

function RecordingTimeline({
	playlist,
	currentPart,
	currentSeconds,
	onSeek,
	popoverSide = "top",
	showTimeLabels = true,
	waveformLoading,
	waveformPeaks,
}: {
	playlist: RecordingPlaylist;
	currentPart: RecordingPlaylistPart;
	currentSeconds: number;
	onSeek: (seconds: number, options?: RecordingSeekOptions) => void;
	popoverSide?: TimelinePopoverSide;
	showTimeLabels?: boolean;
	waveformLoading?: boolean;
	waveformPeaks?: number[] | null;
}) {
	const { t } = useTranslation();
	const total = playlist.totalDurationSeconds;
	const hasDuration = total > 0;
	const progress = percentOf(currentSeconds, total);
	const hasWaveform = !!waveformPeaks && waveformPeaks.length > 0;
	const showWaveformLane = hasWaveform || !!waveformLoading;
	const lane = TIMELINE_LANE[showWaveformLane ? "waveform" : "compact"];
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
		<div className="vds-recording-time-slider w-full min-w-0 px-[2px]">
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
			<div className={cn("relative", lane.heightClass)}>
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
								popoverSide={popoverSide}
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
								belowLane={lane.belowLane}
								popoverSide={popoverSide}
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
	const clipRight = 100 - clamp(progress, 0, 100);

	return (
		<div
			className="rv-recording-waveform pointer-events-none absolute inset-0 overflow-hidden rounded-[0.375rem]"
			data-testid="audio-waveform"
			aria-hidden="true"
		>
			<svg viewBox={viewBox} preserveAspectRatio="none" aria-hidden="true">
				<path d={path} />
			</svg>
			<div
				className="rv-recording-waveform-progress absolute inset-0 overflow-hidden"
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
			className="rv-recording-waveform rv-recording-waveform-loading pointer-events-none absolute inset-0 overflow-hidden rounded-[0.375rem] opacity-55"
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
	popoverSide,
	totalSeconds,
	onSeek,
}: {
	getTrackElement: () => HTMLElement | null;
	hasWaveform: boolean;
	part: RecordingPlaylistPart;
	popoverSide: TimelinePopoverSide;
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
					...(popoverSide === "bottom"
						? { top: "calc(100% + 0.5rem)" }
						: { bottom: "calc(100% + 0.5rem)" }),
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
	belowLane,
	marker,
	popoverSide,
	totalSeconds,
	onSeek,
}: {
	belowLane: string;
	marker: RecordingTimelineMarker;
	popoverSide: TimelinePopoverSide;
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
				style={
					popoverSide === "bottom"
						? { top: belowLane }
						: { bottom: "calc(100% + 0.5rem)" }
				}
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
