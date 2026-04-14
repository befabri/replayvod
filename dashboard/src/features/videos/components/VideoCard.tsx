import { Link } from "@tanstack/react-router";
import { useEffect, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { Avatar } from "@/components/ui/avatar";
import { API_URL } from "@/env";
import { useChannel } from "@/features/channels";
import { useVideoSnapshots, type VideoResponse } from "@/features/videos";
import { formatDuration } from "@/features/videos/format";
import { CategoryHistoryButton } from "./CategoryHistoryButton";
import { TitleHistoryButton } from "./TitleHistoryButton";

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

// InlineStatusBadge renders the primary status indicator. When the
// video's completion_kind is "cancelled" we render a grey CANCELLED
// badge instead of the red FAILED — operator-initiated stops aren't
// crashes and shouldn't look like them. "partial" surfaces as a
// separate sub-badge via InlineCompletionBadge.
function InlineStatusBadge({
	status,
	completionKind,
}: {
	status: string;
	completionKind: string;
}) {
	const { t } = useTranslation();
	if (status === "FAILED" && completionKind === "cancelled") {
		return (
			<span className="px-2 py-0.5 rounded-md text-xs font-medium bg-muted text-muted-foreground">
				{t("videos.status.CANCELLED", "CANCELLED")}
			</span>
		);
	}
	const color =
		status === "DONE"
			? "bg-badge-green-bg text-badge-green-fg"
			: status === "FAILED"
				? "bg-badge-red-bg text-badge-red-fg"
				: status === "RUNNING"
					? "bg-badge-blue-bg text-badge-blue-fg"
					: "bg-muted text-muted-foreground";
	return (
		<span
			className={`px-2 py-0.5 rounded-md text-xs font-medium ${color}`}
		>
			{t(`videos.status.${status}` as const, status)}
		</span>
	);
}

// InlineCompletionBadge renders a PARTIAL sub-badge for DONE videos
// whose content is incomplete (typically a shutdown-resume gap that
// the CDN window rolled past). Nothing renders for clean or
// cancelled — the primary badge carries the message there.
function InlineCompletionBadge({
	status,
	completionKind,
}: {
	status: string;
	completionKind: string;
}) {
	const { t } = useTranslation();
	if (status !== "DONE" || completionKind !== "partial") return null;
	return (
		<span
			className="px-2 py-0.5 rounded-md text-xs font-medium bg-badge-yellow-bg text-badge-yellow-fg"
			title={t(
				"videos.completion.partial_tooltip",
				"Some of this recording was lost — the download was interrupted and the Twitch CDN rolled past the gap before we resumed.",
			)}
		>
			{t("videos.completion.partial", "PARTIAL")}
		</span>
	);
}

function ThumbnailOverlay({ children }: { children: React.ReactNode }) {
	return (
		<span className="px-2 py-0.5 rounded-md text-xs font-medium bg-overlay text-white">
			{children}
		</span>
	);
}

export function VideoCard({ video }: { video: VideoResponse }) {
	const { t } = useTranslation();
	const { data: channel } = useChannel(video.broadcaster_id);
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

	const channelLabel = channel?.broadcaster_name ?? video.broadcaster_id;
	const dateLabel = new Date(video.start_download_at).toLocaleDateString();
	// Stream title is the thing the streamer typed as the broadcast
	// label — the primary line in the card. Falls back to the
	// channel display name when Helix didn't surface one (manual
	// trigger against a channel that just went offline, title
	// tracking disabled, etc.).
	const primaryLabel = video.title?.trim() || video.display_name;

	// The TitleHistoryButton is rendered as a sibling of the Link
	// below, not inside it: the button must swallow clicks so it
	// doesn't trigger navigation to /watch.
	const body = (
		<>
			<div
				className="aspect-video bg-muted flex items-center justify-center relative"
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
						className="w-full h-full object-cover"
						loading="lazy"
					/>
				) : (
					<div className="text-muted-foreground text-sm">
						{t("videos.no_thumbnail")}
					</div>
				)}
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
								className="absolute inset-0 w-full h-full object-cover"
							/>
						)}
						{/* Current frame fades in on top. `key` on src
						    forces a remount on each swap so the CSS
						    enter animation replays. */}
						<img
							key={snapshotFrame.current}
							src={snapshotFrame.current}
							alt=""
							className="absolute inset-0 w-full h-full object-cover animate-in fade-in-0 duration-300"
						/>
					</>
				)}
				{/* Duration (top-left) + date (bottom-right) overlays match v1. */}
				<span className="absolute top-2 left-2">
					<ThumbnailOverlay>{formatDuration(video.duration_seconds)}</ThumbnailOverlay>
				</span>
				<span className="absolute bottom-2 right-2">
					<ThumbnailOverlay>{dateLabel}</ThumbnailOverlay>
				</span>
				{/* Top-right stack: categories + titles (dialogs) +
				    status badges. Categories and titles live on the
				    card so users can inspect them without having to
				    navigate into the video. */}
				<div className="absolute top-2 right-2 flex items-center gap-1.5">
					{video.status === "DONE" && (
						<>
							<CategoryHistoryButton videoId={video.id} />
							<TitleHistoryButton videoId={video.id} />
						</>
					)}
					<InlineCompletionBadge
						status={video.status}
						completionKind={video.completion_kind}
					/>
					<InlineStatusBadge
						status={video.status}
						completionKind={video.completion_kind}
					/>
				</div>
			</div>
			<div className="p-3 flex flex-col gap-2">
				<div
					className="font-medium line-clamp-2 leading-snug"
					title={primaryLabel}
				>
					{primaryLabel}
				</div>
				<div className="flex items-center gap-2 text-sm text-muted-foreground">
					<Avatar
						src={channel?.profile_image_url}
						name={channelLabel}
						alt={channelLabel}
						size="sm"
					/>
					<span className="truncate">{channelLabel}</span>
				</div>
			</div>
		</>
	);

	if (video.status === "DONE") {
		return (
			<Link
				// biome-ignore lint/suspicious/noExplicitAny: param route typing
				to={"/dashboard/watch/$videoId" as any}
				params={{ videoId: String(video.id) } as any}
				className="rounded-lg bg-card overflow-hidden flex flex-col shadow-sm hover:bg-accent transition-colors duration-75"
			>
				{body}
			</Link>
		);
	}
	return (
		<div className="rounded-lg bg-card overflow-hidden flex flex-col shadow-sm">
			{body}
		</div>
	);
}
