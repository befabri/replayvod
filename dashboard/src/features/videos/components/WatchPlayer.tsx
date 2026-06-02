import "@vidstack/react/player/styles/default/theme.css";
import "@vidstack/react/player/styles/default/layouts/video.css";
import "./WatchPlayer.css";

import {
	MediaPlayer,
	type MediaPlayerInstance,
	MediaProvider,
	type PlayerSrc,
	Track,
	type VTTContent,
} from "@vidstack/react";
import {
	DefaultVideoLayout,
	defaultLayoutIcons,
} from "@vidstack/react/player/layouts/default";
import {
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
	playlist,
	initialOffsetSeconds,
}: {
	playlist: RecordingPlaylist;
	initialOffsetSeconds?: number;
}) {
	const isCrossOrigin = !!API_URL;
	const playerRef = useRef<MediaPlayerInstance>(null);
	const pendingSeekRef = useRef<{
		localSeconds: number;
		playbackRate: number;
		resume: boolean;
	} | null>(null);
	const appliedInitialSeekRef = useRef<string | null>(null);
	const wasPlayingRef = useRef(false);
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
						type: playlist.continuousSource.mimeType,
					} as PlayerSrc)
				: currentPart
					? ({
							src: currentPart.src,
							type: currentPart.mimeType,
						} as PlayerSrc)
					: undefined,
		[currentPart, playlist.continuousSource, usesContinuousSource],
	);
	// Stable string identity of the loaded source (PlayerSrc is an opaque vidstack
	// union). Drives readiness tracking and source-swap detection.
	const currentSourceKey =
		usesContinuousSource && playlist.continuousSource
			? playlist.continuousSource.src
			: currentPart?.src;
	// The continuous (muxed) file plays on its own clock; this is its probed
	// duration when known, used to map player time onto the recording timeline.
	const continuousDurationSeconds = usesContinuousSource
		? (playlist.continuousSource?.durationSeconds ?? null)
		: null;

	const seekToGlobal = useCallback(
		(offsetSeconds: number, options?: { resume?: boolean }) => {
			const target = findPartForOffset(
				playlist.parts,
				offsetSeconds,
				playlist.totalDurationSeconds,
			);
			if (!target) return;
			const player = playerRef.current;
			const resume = options?.resume ?? (player ? !player.paused : false);
			const sourceReady = readySourceKeyRef.current === currentSourceKey;
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
				if (player && sourceReady) {
					pendingSeekRef.current = null;
					player.currentTime = playerSeconds;
					if (resume) void player.play().catch(() => {});
				} else {
					// Media not ready (cold load / deep link); defer the player offset.
					pendingSeekRef.current = {
						localSeconds: playerSeconds,
						playbackRate: player?.playbackRate ?? 1,
						resume,
					};
				}
				return;
			}
			if (target.part.position === partPosition && player) {
				if (sourceReady) {
					pendingSeekRef.current = null;
					player.currentTime = target.localSeconds;
					if (resume) void player.play().catch(() => {});
				} else {
					pendingSeekRef.current = {
						localSeconds: target.localSeconds,
						playbackRate: player.playbackRate ?? 1,
						resume,
					};
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
			partPosition,
			playlist.parts,
			playlist.totalDurationSeconds,
			usesContinuousSource,
		],
	);

	// biome-ignore lint/correctness/useExhaustiveDependencies: reset local playback state when the playlist identity changes.
	useEffect(() => {
		setForcePartSequencer(false);
		setPartPosition(0);
		setGlobalTime(0);
		pendingSeekRef.current = null;
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
			playbackRate: playerRef.current?.playbackRate ?? 1,
			resume: wasPlayingRef.current,
		};
	}, [
		usesContinuousSource,
		globalTime,
		playlist.totalDurationSeconds,
		continuousDurationSeconds,
	]);

	useEffect(() => {
		if (initialOffsetSeconds == null) return;
		const key = `${playlist.videoId}:${initialOffsetSeconds}`;
		if (appliedInitialSeekRef.current === key) return;
		appliedInitialSeekRef.current = key;
		seekToGlobal(initialOffsetSeconds, { resume: false });
	}, [initialOffsetSeconds, playlist.videoId, seekToGlobal]);

	if (!currentPart || !currentSource) return null;

	function handleCanPlay() {
		// The source is now seekable; record its identity so seeks apply immediately.
		readySourceKeyRef.current = currentSourceKey;
		const pending = pendingSeekRef.current;
		if (!pending || !playerRef.current) return;
		pendingSeekRef.current = null;
		playerRef.current.currentTime = pending.localSeconds;
		playerRef.current.playbackRate = pending.playbackRate;
		if (pending.resume) void playerRef.current.play().catch(() => {});
	}

	function handleError() {
		if (!usesContinuousSource || playlist.parts.length <= 1) return;
		const player = playerRef.current;
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

	function handleEnded() {
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
			playbackRate: playerRef.current?.playbackRate ?? 1,
			resume: true,
		};
		setGlobalTime(next.startSeconds);
		setPartPosition(next.position);
	}

	function handleTimeUpdate(detail: { currentTime: number }) {
		if (playerRef.current) {
			wasPlayingRef.current = !playerRef.current.paused;
		}
		setGlobalTime(
			usesContinuousSource
				? playerTimeToCanonical(
						detail.currentTime,
						playlist.totalDurationSeconds,
						continuousDurationSeconds,
					)
				: globalTimeForPart(currentPart, detail.currentTime),
		);
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
			onTimeUpdate={handleTimeUpdate}
			className="rounded-lg overflow-hidden bg-black shadow-sm"
		>
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
			<DefaultVideoLayout
				icons={defaultLayoutIcons}
				slots={{
					timeSlider: (
						<RecordingTimeline
							playlist={playlist}
							currentPart={currentPart}
							currentSeconds={globalTime}
							onSeek={seekToGlobal}
						/>
					),
				}}
			/>
		</MediaPlayer>
	);
}

function RecordingTimeline({
	playlist,
	currentPart,
	currentSeconds,
	onSeek,
}: {
	playlist: RecordingPlaylist;
	currentPart: RecordingPlaylistPart;
	currentSeconds: number;
	onSeek: (seconds: number) => void;
}) {
	const { t } = useTranslation();
	const total = playlist.totalDurationSeconds;
	const hasDuration = total > 0;
	const progress = percentOf(currentSeconds, total);
	const timelineSeek = useTimelinePointerSeek<HTMLDivElement>({
		totalSeconds: total,
		onSeek,
	});
	const partLabel = t("watch.part_status", {
		current: currentPart.position + 1,
		total: playlist.parts.length,
	});

	return (
		<div className="vds-recording-time-slider">
			<div className="mb-1 grid grid-cols-[auto_1fr_auto] items-center gap-2 text-[11px] tabular-nums text-white/75">
				<span className="min-w-10">{formatPlaybackTime(currentSeconds)}</span>
				<span className="truncate text-center text-[10px] uppercase tracking-wide text-white/60">
					{playlist.parts.length > 1 ? partLabel : ""}
				</span>
				<span className="min-w-10 text-right">{formatPlaybackTime(total)}</span>
			</div>
			<div className="relative h-5">
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
						"absolute inset-x-0 top-[13px] h-1.5 -translate-y-1/2 cursor-pointer rounded-full bg-white/20 outline-none ring-offset-black transition focus-visible:ring-2 focus-visible:ring-primary focus-visible:ring-offset-2",
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
					<span
						className="block h-full rounded-full bg-primary"
						style={{ width: `${progress}%` }}
					/>
				</div>
				{hasDuration &&
					playlist.parts.map((part) => (
						<PartSegmentButton
							key={part.partIndex}
							part={part}
							totalSeconds={total}
							onSeek={onSeek}
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
						className="pointer-events-none absolute top-[13px] size-3 -translate-x-1/2 -translate-y-1/2 rounded-full border-2 border-black bg-primary shadow-sm"
						style={{ left: `${progress}%` }}
					/>
				)}
			</div>
		</div>
	);
}

function PartSegmentButton({
	part,
	totalSeconds,
	onSeek,
}: {
	part: RecordingPlaylistPart;
	totalSeconds: number;
	onSeek: (seconds: number) => void;
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
		getTrackElement: parentElementOrSelf,
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
				"group absolute top-[13px] z-10 h-5 min-w-2 -translate-y-1/2 overflow-visible rounded-sm border-0 bg-transparent p-0 outline-none ring-offset-black transition hover:bg-white/10 focus-visible:ring-2 focus-visible:ring-primary focus-visible:ring-offset-2",
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
				className="pointer-events-none absolute hidden w-max max-w-[min(22rem,calc(100vw-4rem))] -translate-x-1/2 rounded-md border border-white/15 bg-black/90 px-3 py-2 text-left text-xs leading-snug text-white shadow-xl ring-1 ring-white/10 backdrop-blur group-hover:block group-focus-visible:block"
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
	part,
	totalSeconds,
}: {
	part: RecordingPlaylistPart;
	totalSeconds: number;
}) {
	const left = percentOf(part.startSeconds, totalSeconds);
	return (
		<span
			aria-hidden="true"
			className="pointer-events-none absolute top-[13px] z-20 h-3 w-px -translate-y-1/2 bg-black/70 shadow-[0_0_0_1px_rgb(255_255_255/0.22)]"
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
	onSeek: (seconds: number) => void;
}) {
	const { t } = useTranslation();
	const left = percentOf(marker.offsetSeconds, totalSeconds);
	const time = formatPlaybackTime(marker.offsetSeconds);
	const align = popoverAlign(left);
	return (
		<button
			type="button"
			className={cn(
				"group absolute top-0 z-20 h-2.5 w-1.5 overflow-visible rounded-full border border-black/80 bg-white shadow-sm outline-none ring-offset-black transition hover:h-3.5 hover:w-2 hover:-translate-y-0.5 focus-visible:ring-2 focus-visible:ring-primary focus-visible:ring-offset-2",
				align === "start" && "translate-x-0",
				align === "center" && "-translate-x-1/2",
				align === "end" && "-translate-x-full",
			)}
			style={{ left: `${left}%` }}
			aria-label={t("watch.seek_marker", {
				label: marker.label,
				time,
			})}
			onClick={() => onSeek(marker.offsetSeconds)}
		>
			<span
				className={cn(
					"pointer-events-none absolute hidden w-max max-w-[min(24rem,calc(100vw-4rem))] rounded-md border border-white/15 bg-black/90 px-3 py-2 text-left text-xs leading-snug text-white shadow-xl ring-1 ring-white/10 backdrop-blur group-hover:block group-focus-visible:block",
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
	onSeek: (seconds: number) => void;
	getTrackElement?: (element: T) => HTMLElement | null;
}) {
	const draggingRef = useRef(false);
	const seekFromEvent = useCallback(
		(event: MouseEvent<T> | PointerEvent<T>) => {
			const trackElement = (getTrackElement ?? selfElement)(
				event.currentTarget,
			);
			if (!trackElement) return;
			seekFromClientX(event.clientX, trackElement, totalSeconds, onSeek);
		},
		[getTrackElement, onSeek, totalSeconds],
	);
	const handleClick = useCallback(
		(event: MouseEvent<T>) => {
			seekFromEvent(event);
		},
		[seekFromEvent],
	);
	const handlePointerDown = useCallback(
		(event: PointerEvent<T>) => {
			if (event.button !== 0) return;
			draggingRef.current = true;
			event.currentTarget.setPointerCapture?.(event.pointerId);
			seekFromEvent(event);
		},
		[seekFromEvent],
	);
	const handlePointerMove = useCallback(
		(event: PointerEvent<T>) => {
			if (!draggingRef.current) return;
			seekFromEvent(event);
		},
		[seekFromEvent],
	);
	const endPointerDrag = useCallback(
		(event: PointerEvent<T>) => {
			if (!draggingRef.current) return;
			draggingRef.current = false;
			event.currentTarget.releasePointerCapture?.(event.pointerId);
			seekFromEvent(event);
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

function seekFromClientX(
	clientX: number,
	trackElement: HTMLElement,
	totalSeconds: number,
	onSeek: (seconds: number) => void,
) {
	if (totalSeconds <= 0) return;
	const rect = trackElement.getBoundingClientRect();
	const ratio =
		rect.width <= 0 ? 0 : clamp((clientX - rect.left) / rect.width, 0, 1);
	onSeek(ratio * totalSeconds);
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

function selfElement<T extends HTMLElement>(element: T): HTMLElement {
	return element;
}

function parentElementOrSelf(element: HTMLElement): HTMLElement {
	return element.parentElement ?? element;
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
