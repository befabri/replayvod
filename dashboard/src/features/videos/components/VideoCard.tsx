import { PlayIcon } from "@phosphor-icons/react";
import { Link } from "@tanstack/react-router";
import type { TFunction } from "i18next";
import { type ComponentProps, useEffect, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { Avatar } from "@/components/ui/avatar";
import { Badge } from "@/components/ui/badge";
import { usePlaybackSettings } from "@/features/settings/playback";
import {
	channelLabel,
	useVideoSnapshots,
	type VideoResponse,
} from "@/features/videos";
import {
	type VideoCardBadgeTone,
	videoCardStatusBadge,
} from "@/features/videos/card-badges";
import {
	formatBytes,
	formatDuration,
	formatPlaybackTime,
} from "@/features/videos/format";
import { videoQualityLabel } from "@/features/videos/labels";
import { resumeOffsetSeconds } from "@/features/videos/resume-policy";
import {
	firstSnapshotPath,
	localThumbnailURL,
} from "@/features/videos/thumbnail";
import { clamp, cn } from "@/lib/utils";
import { RemoveVideoButton } from "./RemoveVideoButton";
import { StreamHistoryButton } from "./StreamHistoryButton";
import { WatchLaterButton } from "./WatchLaterButton";

const HOVER_SWAP_INTERVAL_MS = 900;

const HOVER_INTENT_DELAY_MS = 700;

const MAX_CARD_TAGS = 3;

const STORED_PREVIEW_RETRY_DELAY_MS = 5000;
const STORED_PREVIEW_MAX_RETRIES = 3;
const STORED_PREVIEW_VISIBILITY_ROOT_MARGIN = "300px 0px";

const OVERLAY_BADGE_TONES: Record<VideoCardBadgeTone, string> = {
	neutral: "bg-black/60 text-white/80",
	blue: "bg-badge-blue-bg/70 text-badge-blue-fg",
	red: "bg-badge-red-bg/70 text-badge-red-fg",
	yellow: "bg-badge-yellow-bg/70 text-badge-yellow-fg",
};

function cacheBustedURL(url: string, cacheBust: number): string {
	return cacheBust > 0 ? `${url}?rv=${cacheBust}` : url;
}

function firstSnapshotURL(video: VideoResponse, cacheBust = 0): string {
	return cacheBustedURL(
		localThumbnailURL(firstSnapshotPath(video.filename)),
		cacheBust,
	);
}

function useHoverSnapshots(
	urls: string[],
	active: boolean,
): { current: string; prev: string | null } | null {
	const [index, setIndex] = useState(0);
	const [hasTicked, setHasTicked] = useState(false);

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

function StatusOverlayBadge({
	video,
	t,
}: {
	video: VideoResponse;
	t: TFunction;
}) {
	const badge = videoCardStatusBadge(video);
	if (!badge) return null;
	return (
		<ThumbnailOverlay
			tone={badge.tone}
			title={badge.tooltipKey ? t(badge.tooltipKey) : undefined}
			data-testid="video-card-status"
		>
			{t(badge.labelKey)}
		</ThumbnailOverlay>
	);
}

function ThumbnailOverlay({
	className,
	tone = "neutral",
	...props
}: ComponentProps<"span"> & { tone?: VideoCardBadgeTone }) {
	return (
		<span
			className={cn(
				"rounded px-1.5 py-0.5 text-[11px] font-medium leading-4 backdrop-blur-md",
				OVERLAY_BADGE_TONES[tone],
				className,
			)}
			{...props}
		/>
	);
}

export function VideoCard({
	video,
	canManage,
}: {
	video: VideoResponse;
	canManage: boolean;
}) {
	const { t } = useTranslation();
	const policy = usePlaybackSettings();
	const mediaRef = useRef<HTMLDivElement | null>(null);
	const [mediaVisible, setMediaVisible] = useState(false);
	const thumbnail = video.thumbnail ? localThumbnailURL(video.thumbnail) : null;
	const [storedPreviewRetry, setStoredPreviewRetry] = useState(0);
	const [storedPreviewRetryPending, setStoredPreviewRetryPending] =
		useState(false);
	const [storedPreviewFailed, setStoredPreviewFailed] = useState(false);
	const storedPreviewThumbnail =
		!thumbnail &&
		!storedPreviewFailed &&
		(video.status === "DONE" || mediaVisible)
			? firstSnapshotURL(video, storedPreviewRetry)
			: null;
	const fallbackThumbnail =
		storedPreviewFailed || storedPreviewRetryPending
			? null
			: storedPreviewThumbnail;
	const heroThumbnail = thumbnail ?? fallbackThumbnail;
	const fallbackIdentity = [video.id, video.filename, video.status].join(":");

	useEffect(() => {
		const node = mediaRef.current;
		if (!node) return;
		if (typeof window === "undefined" || !("IntersectionObserver" in window)) {
			setMediaVisible(true);
			return;
		}
		const observer = new window.IntersectionObserver(
			(entries) => {
				setMediaVisible(entries.some((entry) => entry.isIntersecting));
			},
			{ rootMargin: STORED_PREVIEW_VISIBILITY_ROOT_MARGIN },
		);
		observer.observe(node);
		return () => observer.disconnect();
	}, []);

	useEffect(() => {
		if (!fallbackIdentity) return;
		setStoredPreviewRetry(0);
		setStoredPreviewRetryPending(false);
		setStoredPreviewFailed(false);
	}, [fallbackIdentity]);

	useEffect(() => {
		if (!storedPreviewRetryPending || !mediaVisible) return;
		const id = window.setTimeout(() => {
			setStoredPreviewRetry((retry) => retry + 1);
			setStoredPreviewRetryPending(false);
		}, STORED_PREVIEW_RETRY_DELAY_MS);
		return () => window.clearTimeout(id);
	}, [storedPreviewRetryPending, mediaVisible]);

	function handleFallbackThumbnailError() {
		if (storedPreviewThumbnail && heroThumbnail === storedPreviewThumbnail) {
			if (
				video.status !== "DONE" &&
				storedPreviewRetry < STORED_PREVIEW_MAX_RETRIES
			) {
				setStoredPreviewRetryPending(true);
				return;
			}
			setStoredPreviewFailed(true);
		}
	}

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

	const { data: snapshotPaths } = useVideoSnapshots(
		video.id,
		previewing && video.status === "DONE",
	);
	const snapshotURLs = (snapshotPaths ?? []).map(localThumbnailURL);
	const snapshotFrame = useHoverSnapshots(snapshotURLs, previewing);

	const label = channelLabel(video);
	const dateLabel = new Date(video.start_download_at).toLocaleDateString();
	const isArchive = video.source === "vod";
	const airedLabel =
		isArchive && video.broadcast_at
			? new Date(video.broadcast_at).toLocaleDateString()
			: null;
	const sizeLabel = formatBytes(video.size_bytes);
	const primaryCategoryLabel = video.primary_category_name?.trim() || null;
	const primaryLabel = video.title?.trim() || video.display_name;
	const resumeSeconds =
		video.status === "DONE"
			? resumeOffsetSeconds(
					video.user_state,
					video.duration_seconds ?? 0,
					policy,
				)
			: undefined;
	const resumePercent =
		resumeSeconds != null && video.duration_seconds
			? clamp((resumeSeconds / video.duration_seconds) * 100, 0, 100)
			: null;

	const media = (
		<>
			{/* biome-ignore lint/a11y/noStaticElementInteractions: hover intent drives preview loading, not button-like interaction */}
			<div
				ref={mediaRef}
				className="relative flex aspect-video items-center justify-center overflow-hidden rounded-xl bg-muted"
				onMouseEnter={() => setHovered(true)}
				onMouseLeave={() => setHovered(false)}
			>
				{heroThumbnail ? (
					<img
						src={heroThumbnail}
						alt=""
						className="h-full w-full object-cover"
						loading="lazy"
						onError={thumbnail ? undefined : handleFallbackThumbnailError}
					/>
				) : (
					<div className="text-muted-foreground text-sm">
						{t("videos.no_thumbnail")}
					</div>
				)}
				<div className="pointer-events-none absolute inset-0 bg-gradient-to-t from-black/60 via-black/10 to-black/5" />
				{snapshotFrame && (
					<>
						{snapshotFrame.prev && (
							<img
								src={snapshotFrame.prev}
								alt=""
								className="absolute inset-0 h-full w-full object-cover"
							/>
						)}
						<img
							key={snapshotFrame.current}
							src={snapshotFrame.current}
							alt=""
							className="absolute inset-0 h-full w-full object-cover animate-in fade-in-0 duration-300"
						/>
					</>
				)}
				<div
					className="absolute top-2 left-2 flex items-center gap-1"
					data-testid="video-card-badges"
				>
					<ThumbnailOverlay data-testid="video-card-quality">
						{videoQualityLabel(t, video)}
					</ThumbnailOverlay>
					<StatusOverlayBadge video={video} t={t} />
				</div>
				{video.duration_seconds ? (
					<span className="absolute bottom-2 left-2">
						<ThumbnailOverlay>
							{formatDuration(video.duration_seconds)}
						</ThumbnailOverlay>
					</span>
				) : null}
				<span className="absolute bottom-2 right-2">
					<ThumbnailOverlay>
						<span
							data-testid="video-card-date"
							title={
								airedLabel
									? t("videos.streamed_on", { date: airedLabel })
									: undefined
							}
						>
							{airedLabel ?? dateLabel}
						</span>
					</ThumbnailOverlay>
				</span>
				{resumePercent != null && resumeSeconds != null ? (
					<div
						className="absolute inset-x-0 bottom-0 h-1 bg-white/25"
						role="progressbar"
						aria-valuemin={0}
						aria-valuemax={100}
						aria-valuenow={Math.round(resumePercent)}
						aria-label={t("videos.resume_progress", {
							time: formatPlaybackTime(resumeSeconds),
						})}
						data-testid="video-card-progress"
					>
						<div
							className="h-full bg-primary"
							style={{ width: `${resumePercent}%` }}
						/>
					</div>
				) : null}
				{video.status === "DONE" ? (
					<div
						className="pointer-events-none absolute inset-0 flex items-center justify-center"
						data-testid="video-card-play"
					>
						<div className="flex size-11 items-center justify-center rounded-full bg-background/90 text-foreground shadow-sm opacity-0 transition-opacity duration-150 group-hover/video-card:opacity-100">
							<PlayIcon weight="fill" className="size-4 translate-x-px" />
						</div>
					</div>
				) : null}
			</div>
		</>
	);

	const mediaLinkClassName =
		"block rounded-xl focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2";
	const mediaNode =
		video.status === "DONE" ? (
			<Link
				to="/dashboard/watch/$videoId"
				params={{ videoId: String(video.id) }}
				search={{ t: undefined }}
				className={mediaLinkClassName}
				aria-label={t("videos.watch_recording", { title: primaryLabel })}
			>
				{media}
			</Link>
		) : video.status === "FAILED" ? (
			<Link
				to="/dashboard/activity/history"
				search={{
					outcome:
						video.completion_kind === "cancelled" ? "cancelled" : "failed",
					media: "any",
				}}
				className={mediaLinkClassName}
				aria-label={t("videos.open_history", { title: primaryLabel })}
			>
				{media}
			</Link>
		) : (
			<Link
				to="/dashboard/downloads"
				className={mediaLinkClassName}
				aria-label={t("videos.open_downloads", { title: primaryLabel })}
			>
				{media}
			</Link>
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
							search={{ t: undefined }}
							className="min-w-0 flex-1 line-clamp-2 font-medium leading-snug text-foreground transition-colors hover:text-link focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2"
						>
							{primaryLabel}
						</Link>
						<StreamHistoryButton
							videoId={video.id}
							videoStartDownloadAt={video.start_download_at}
							t={t}
						/>
						<WatchLaterButton
							videoId={video.id}
							watchLater={video.user_state?.watch_later ?? false}
						/>
						{canManage ? <RemoveVideoButton videoId={video.id} /> : null}
					</div>
				) : (
					<div className="flex items-start gap-2">
						<div
							className="min-w-0 flex-1 line-clamp-2 font-medium leading-snug"
							title={primaryLabel}
						>
							{primaryLabel}
						</div>
						<WatchLaterButton
							videoId={video.id}
							watchLater={video.user_state?.watch_later ?? false}
						/>
					</div>
				)}
				<div className="flex items-center gap-2.5 text-sm text-muted-foreground">
					<Link
						to="/dashboard/channels/$channelId"
						params={{ channelId: video.broadcaster_id }}
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
							to="/dashboard/channels/$channelId"
							params={{ channelId: video.broadcaster_id }}
							className="block truncate font-medium text-foreground transition-colors hover:text-link focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2"
						>
							{label}
						</Link>
						<div className="flex flex-wrap items-center gap-2 text-xs text-muted-foreground">
							<span>{sizeLabel}</span>
							{isArchive ? (
								<>
									<span className="opacity-40">·</span>
									<span data-testid="video-card-archive">
										{t("videos.archive_badge")}
									</span>
								</>
							) : null}
							{video.status === "DONE" && primaryCategoryLabel ? (
								<>
									<span className="opacity-40">·</span>
									{video.primary_category_id ? (
										<Link
											to="/dashboard/categories/$categoryId"
											params={{ categoryId: video.primary_category_id }}
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
				<div className="h-5.5">
					{video.tags?.length ? (
						<ul
							className="flex h-full items-center gap-1 overflow-hidden"
							aria-label={t("videos.tags")}
							title={video.tags.map((tag) => tag.name).join(", ")}
							data-testid="video-card-tags"
						>
							{video.tags.slice(0, MAX_CARD_TAGS).map((tag) => (
								<li key={tag.id} className="min-w-0 max-w-32">
									<Badge variant="muted" className="max-w-full">
										<span className="truncate">{tag.name}</span>
									</Badge>
								</li>
							))}
							{video.tags.length > MAX_CARD_TAGS ? (
								<li className="shrink-0">
									<Badge variant="muted">
										+{video.tags.length - MAX_CARD_TAGS}
									</Badge>
								</li>
							) : null}
						</ul>
					) : null}
				</div>
			</div>
		</div>
	);
}
