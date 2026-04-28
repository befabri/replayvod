import { Play } from "@phosphor-icons/react";
import { Link } from "@tanstack/react-router";
import { useEffect, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { Avatar } from "@/components/ui/avatar";
import { API_URL } from "@/env";
import {
	channelLabel,
	useVideoSnapshots,
	type VideoResponse,
} from "@/features/videos";
import { formatBytes, formatDuration } from "@/features/videos/format";
import { StreamHistoryButton } from "./StreamHistoryButton";

// Ms between snapshot swaps on hover. 900ms — middle-ground between
// snappy (600ms, registered as flicker) and sedate (1200ms, felt slow):
// readable per-frame, not lagging.
const HOVER_SWAP_INTERVAL_MS = 900;

// Ms of sustained hover before the preview cycle starts. Filters out
// the mouse sweeping across the grid — users who genuinely stop to
// look at a card cross this threshold without noticing it, brief
// pass-throughs don't trigger snapshot fetches or cycling.
const HOVER_INTENT_DELAY_MS = 700;

// useHoverSnapshots cycles through a list of snapshot URLs while
// `active` is true. Returns { current, prev } so the caller can layer
// them for a crossfade — the prev frame stays visible under the
// fading-in current, eliminating the brief "flash of hero thumbnail"
// between swaps.
//
// `prev` is null on the very first frame after activation: there's no
// prior snapshot to fade from, so the caller should fade `current`
// straight over whatever sits behind it (hero thumbnail). Without this
// the first crossfade would go `urls[N-1]` → `urls[0]` via the wrap-
// around modulo, which reads as a jarring "flash of last frame" the
// instant hover intent resolves. Resets on every re-activation.
function useHoverSnapshots(
	urls: string[],
	active: boolean,
): { current: string; prev: string | null } | null {
	const [index, setIndex] = useState(0);
	const [hasTicked, setHasTicked] = useState(false);
	const activeRef = useRef(active);
	activeRef.current = active;

	useEffect(() => {
		if (!active || urls.length === 0) {
			setIndex(0);
			setHasTicked(false);
			return;
		}
		const id = window.setInterval(() => {
			setIndex((i) => (i + 1) % urls.length);
			setHasTicked(true);
		}, HOVER_SWAP_INTERVAL_MS);
		return () => window.clearInterval(id);
	}, [active, urls.length]);

	if (!active || urls.length === 0) return null;
	const current = urls[index];
	if (!current) return null;
	if (!hasTicked) return { current, prev: null };
	const prev = urls[(index - 1 + urls.length) % urls.length];
	return { current, prev: prev ?? null };
}

// IncompleteOverlayBadge marks recordings that didn't capture the
// full broadcast. One yellow badge with three labels — same visual
// bucket because the user-facing concept ("this isn't the whole
// stream") is the same; the wording just clarifies *why* it isn't:
//
//   PARTIAL   — completion_kind='partial'. File has gaps inside it
//               (CDN window-roll, or run failed mid-recording with
//               only some parts saved). Highest-impact case: data
//               is actually missing from inside the file.
//   CANCELLED — completion_kind='cancelled'. Operator stopped the
//               run while the broadcast was still live. File plays
//               cleanly, but distinct user intent ("I did this on
//               purpose") earns its own label.
//   TRUNCATED — fallback for everything else with truncated=true:
//               recorder ended without operator action (server
//               restart with no resume, finalize without
//               EXT-X-ENDLIST, etc.). File plays cleanly; the
//               broadcast just continued past where we stopped.
function IncompleteOverlayBadge({
	completionKind,
	truncated,
}: {
	completionKind: string;
	truncated: boolean;
}) {
	const { t } = useTranslation();
	const variant =
		completionKind === "partial"
			? "partial"
			: completionKind === "cancelled"
				? "cancelled"
				: truncated
					? "truncated"
					: null;
	if (!variant) return null;
	return (
		<span
			className="rounded-md bg-badge-yellow-bg/85 px-2 py-0.5 text-xs font-medium text-badge-yellow-fg backdrop-blur-sm"
			title={t(`videos.completion.${variant}_tooltip` as const)}
		>
			{t(`videos.completion.${variant}` as const)}
		</span>
	);
}

// ThumbnailOverlay is the neutral pill used for duration/date in the
// thumbnail's bottom row. The quality badge in the top-left uses its
// own accent-tinted variant via QualityOverlay below.
function ThumbnailOverlay({ children }: { children: React.ReactNode }) {
	return (
		<span className="rounded-md border border-border/60 bg-background/78 px-2 py-0.5 text-xs font-medium text-white backdrop-blur-sm">
			{children}
		</span>
	);
}

function QualityOverlay({ children }: { children: React.ReactNode }) {
	return (
		<span className="rounded-md border border-border/60 bg-background/78 px-2 py-0.5 text-xs font-medium text-primary backdrop-blur-sm">
			{children}
		</span>
	);
}

export function VideoCard({ video }: { video: VideoResponse }) {
	const { t } = useTranslation();
	const thumbnail = video.thumbnail
		? `${API_URL}/api/v1/thumbnails/${video.thumbnail.replace(/^thumbnails\//, "")}`
		: null;

	// Hover intent: only mark `previewing` true after the user has
	// held hover for HOVER_INTENT_DELAY_MS. Cancels on mouseleave so
	// a pointer passing through the grid doesn't fan out snapshot
	// fetches or start cycle animations.
	const [hovered, setHovered] = useState(false);
	const [previewing, setPreviewing] = useState(false);
	useEffect(() => {
		if (!hovered) {
			setPreviewing(false);
			return;
		}
		const id = window.setTimeout(
			() => setPreviewing(true),
			HOVER_INTENT_DELAY_MS,
		);
		return () => window.clearTimeout(id);
	}, [hovered]);

	// Only DONE recordings have snapshots saved; we gate the fetch on
	// `previewing` (not raw hover) so a list view doesn't fan out N
	// storage-probe requests just from the pointer crossing the grid.
	// Once a card earns its fetch the result is cached forever
	// (staleTime: Infinity).
	const { data: snapshotPaths } = useVideoSnapshots(
		video.id,
		previewing && video.status === "DONE",
	);
	const snapshotURLs = (snapshotPaths ?? []).map(
		(p) => `${API_URL}/api/v1/thumbnails/${p.replace(/^thumbnails\//, "")}`,
	);
	const snapshotFrame = useHoverSnapshots(snapshotURLs, previewing);

	const label = channelLabel(video);
	const dateLabel = new Date(video.start_download_at).toLocaleDateString();
	const sizeLabel = formatBytes(video.size_bytes);
	const primaryCategoryLabel = video.primary_category_name?.trim() || null;
	// Stream title is the thing the streamer typed as the broadcast
	// label — the primary line in the card. Falls back to the
	// channel display name when Helix didn't surface one (manual
	// trigger against a channel that just went offline, title
	// tracking disabled, etc.).
	const primaryLabel = video.title?.trim() || video.display_name;

	const media = (
		<>
			{/* biome-ignore lint/a11y/noStaticElementInteractions: hover intent drives preview loading, not button-like interaction */}
			<div
				className="relative flex aspect-video items-center justify-center overflow-hidden rounded-xl bg-muted"
				onMouseEnter={() => setHovered(true)}
				onMouseLeave={() => setHovered(false)}
			>
				{/* Hero thumbnail is the always-visible base layer.
				    The snapshot overlay (when present) sits on top
				    and fades in per frame via `key`-forced remount.
				    When no snapshot is active (no hover, no data,
				    or mid-fetch) the hero shows through — that's
				    why the hero layer is rendered unconditionally
				    for DONE recordings and not swapped out. */}
				{thumbnail ? (
					<img
						src={thumbnail}
						alt=""
						className="h-full w-full object-cover"
						loading="lazy"
					/>
				) : (
					<div className="text-muted-foreground text-sm">
						{t("videos.no_thumbnail")}
					</div>
				)}
				<div className="pointer-events-none absolute inset-0 bg-gradient-to-t from-black/60 via-black/10 to-black/5" />
				{snapshotFrame && (
					<>
						{/* Previous frame stays at full opacity as the
						    layer beneath so the current frame crossfades
						    over it instead of revealing the hero thumb.
						    Absent on the very first frame after hover
						    intent resolves — current fades over the
						    hero itself, which is what the user expects
						    when they just arrived at the card. */}
						{snapshotFrame.prev && (
							<img
								src={snapshotFrame.prev}
								alt=""
								className="absolute inset-0 h-full w-full object-cover"
							/>
						)}
						{/* Current frame fades in on top. `key` on src
						    forces a remount on each swap so the CSS
						    enter animation replays. */}
						<img
							key={snapshotFrame.current}
							src={snapshotFrame.current}
							alt=""
							className="absolute inset-0 h-full w-full object-cover animate-in fade-in-0 duration-300"
						/>
					</>
				)}
				{/* Top-left stack: quality + (optional) PARTIAL marker.
				    Duration sits bottom-left, date bottom-right. */}
				<div className="absolute top-2 left-2 flex items-center gap-1.5">
					<QualityOverlay>{video.quality}</QualityOverlay>
					<IncompleteOverlayBadge
						completionKind={video.completion_kind}
						truncated={video.truncated}
					/>
				</div>
				{video.duration_seconds ? (
					<span className="absolute bottom-2 left-2">
						<ThumbnailOverlay>
							{formatDuration(video.duration_seconds)}
						</ThumbnailOverlay>
					</span>
				) : null}
				<span className="absolute bottom-2 right-2">
					<ThumbnailOverlay>{dateLabel}</ThumbnailOverlay>
				</span>
				<div className="pointer-events-none absolute inset-0 flex items-center justify-center">
					<div className="flex size-11 items-center justify-center rounded-full bg-background/90 text-foreground shadow-sm opacity-0 transition-opacity duration-150 group-hover/video-card:opacity-100">
						<Play weight="fill" className="size-4 translate-x-px" />
					</div>
				</div>
			</div>
		</>
	);

	const mediaNode =
		video.status === "DONE" ? (
			<Link
				to="/dashboard/watch/$videoId"
				params={{ videoId: String(video.id) }}
				className="block rounded-xl focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2"
				aria-label={t("videos.watch_recording", { title: primaryLabel })}
			>
				{media}
			</Link>
		) : (
			media
		);

	return (
		<div className="group/video-card flex flex-col transition-transform duration-200 ease-out hover:-translate-y-0.5">
			{mediaNode}
			<div className="flex flex-col gap-2 px-3.5 pt-2 pb-3.5">
				{video.status === "DONE" ? (
					<div className="flex items-start gap-2">
						<Link
							to="/dashboard/watch/$videoId"
							params={{ videoId: String(video.id) }}
							className="min-w-0 flex-1 line-clamp-2 font-medium leading-snug text-foreground transition-colors hover:text-link focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2"
						>
							{primaryLabel}
						</Link>
						<StreamHistoryButton
							videoId={video.id}
							className="inline-flex size-7 shrink-0 items-center justify-center rounded-md text-muted-foreground transition-colors hover:bg-accent hover:text-foreground"
						/>
					</div>
				) : (
					<div
						className="line-clamp-2 font-medium leading-snug"
						title={primaryLabel}
					>
						{primaryLabel}
					</div>
				)}
				<div className="flex items-center gap-2.5 text-sm text-muted-foreground">
					<Link
						// biome-ignore lint/suspicious/noExplicitAny: param route typing
						to={"/dashboard/channels/$channelId" as any}
						// biome-ignore lint/suspicious/noExplicitAny: param route typing
						params={{ channelId: video.broadcaster_id } as any}
						className="inline-flex shrink-0 rounded-full ring-4 ring-transparent ring-offset-2 ring-offset-background transition-[box-shadow,--tw-ring-color] duration-150 hover:ring-accent focus-visible:outline-none focus-visible:ring-ring"
						aria-label={label}
					>
						<Avatar
							src={video.profile_image_url}
							name={label}
							alt={label}
							size="sm"
						/>
					</Link>
					<div className="min-w-0">
						<Link
							// biome-ignore lint/suspicious/noExplicitAny: param route typing
							to={"/dashboard/channels/$channelId" as any}
							// biome-ignore lint/suspicious/noExplicitAny: param route typing
							params={{ channelId: video.broadcaster_id } as any}
							className="block truncate font-medium text-foreground transition-colors hover:text-link focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2"
						>
							{label}
						</Link>
						<div className="flex flex-wrap items-center gap-2 text-xs text-muted-foreground">
							<span>{sizeLabel}</span>
							{video.status === "DONE" && primaryCategoryLabel ? (
								<>
									<span className="opacity-40">·</span>
									{video.primary_category_id ? (
										<Link
											// biome-ignore lint/suspicious/noExplicitAny: param route typing
											to={"/dashboard/categories/$categoryId" as any}
											// biome-ignore lint/suspicious/noExplicitAny: param route typing
											params={{ categoryId: video.primary_category_id } as any}
											className="truncate transition-colors hover:text-foreground"
										>
											{primaryCategoryLabel}
										</Link>
									) : (
										<span className="truncate">{primaryCategoryLabel}</span>
									)}
								</>
							) : null}
						</div>
					</div>
				</div>
			</div>
		</div>
	);
}
