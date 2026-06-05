import { DownloadIcon } from "@phosphor-icons/react";
import { Link } from "@tanstack/react-router";
import { useSelector } from "@tanstack/react-store";
import { useTranslation } from "react-i18next";
import { Avatar } from "@/components/ui/avatar";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { useChannel } from "@/features/channels";
import { useLastLive, useLiveSet } from "@/features/streams-live";
import type { VideoResponse } from "@/features/videos";
import { formatBytes, formatDuration } from "@/features/videos/format";
import {
	isMultipartVideo,
	recordingDurationSeconds,
	recordingQualitySummary,
	recordingSizeBytes,
	videoPartCount,
} from "@/features/videos/metadata";
import {
	useChannelStatistics,
	useVideoCategories,
} from "@/features/videos/queries";
import { useRelativeTime } from "@/lib/format-relative";
import { authStore, hasRole } from "@/stores/auth";
import { TriggerDownloadDialog } from "./TriggerDownloadDialog";

// VideoInfo renders the title block, a chip strip of headline facts,
// and a hairline-bordered channel row. The chip strip mirrors the v1
// reference: quality is the only colored chip, the rest are flat muted
// chips so the eye groups them as one homogeneous block of metadata.
//
// headerAction is an optional slot rendered next to the title — used
// by the watch page to dock the layout toggle alongside the heading
// instead of in a separate toolbar.
export function VideoInfo({
	video,
	headerAction,
}: {
	video: VideoResponse;
	headerAction?: React.ReactNode;
}) {
	const { t } = useTranslation();
	const { data: channel } = useChannel(video.broadcaster_id);
	const { data: categories } = useVideoCategories(video.id);
	const { data: stats } = useChannelStatistics(video.broadcaster_id);
	const { data: lastLive } = useLastLive(video.broadcaster_id);
	// SSE-backed set of currently-live broadcasters — drives the live ring on
	// the channel avatar so the watch page reflects on/offline in real time.
	const isLive = useLiveSet().has(video.broadcaster_id);
	// video.triggerDownload is admin-only on the server, so hide the
	// record-live entry point from viewers instead of failing on submit.
	const canDownload = hasRole(
		useSelector(authStore, (s) => s.user),
		"admin",
	);

	const channelLabel = channel?.broadcaster_name ?? video.broadcaster_id;
	const titleLabel = video.title?.trim() || video.display_name;
	const recordedDate = video.downloaded_at ?? video.start_download_at;
	const partCount = videoPartCount(video);
	const showParts = isMultipartVideo(video);
	const durationSeconds = recordingDurationSeconds(video);
	const sizeBytes = recordingSizeBytes(video);
	const qualityLabel = recordingQualitySummary(
		video,
		t("videos.mixed_quality"),
	);

	// "Last stream" anchors on ended_at (broadcast finished) and falls
	// back to started_at (still mid-broadcast in the local mirror).
	// useRelativeTime ticks once a minute so the label updates without
	// the user having to refresh — formatRelative alone would freeze.
	const lastLiveAt = lastLive?.ended_at || lastLive?.started_at;
	const lastLiveLabel = useRelativeTime(lastLiveAt);
	// Top category by tracked duration. The video may have switched
	// categories mid-stream; the chip surfaces the dominant one rather
	// than VideoResponse.primary_category, which is just whichever was
	// active at download-start.
	const topCategory = categories?.length
		? categories.reduce((a, b) =>
				b.duration_seconds > a.duration_seconds ? b : a,
			)
		: undefined;

	const channelStatLine: string[] = [];
	if (stats && stats.total > 0) {
		channelStatLine.push(
			t("watch.recordings_archived", {
				count: stats.total,
			}),
		);
		if (stats.total_size > 0) {
			channelStatLine.push(formatBytes(stats.total_size));
		}
	}
	if (lastLiveLabel) {
		channelStatLine.push(t("watch.last_stream_ago", { when: lastLiveLabel }));
	}

	return (
		<div className="flex flex-col">
			<div className="flex items-start justify-between gap-3">
				<h1 className="text-lg md:text-xl font-heading font-semibold tracking-tight leading-snug min-w-0">
					{titleLabel}
				</h1>
				{headerAction ? <div className="shrink-0">{headerAction}</div> : null}
			</div>

			<div className="mt-3 flex flex-wrap items-center gap-2">
				<Badge variant="default">{qualityLabel}</Badge>
				{video.language ? (
					<Badge variant="muted" className="uppercase tracking-wide">
						{video.language}
					</Badge>
				) : null}
				{topCategory ? (
					<Link
						to="/dashboard/categories/$categoryId"
						params={{ categoryId: topCategory.id }}
						className="rounded-md hover:opacity-80 transition-opacity"
					>
						<Badge variant="muted">{topCategory.name}</Badge>
					</Link>
				) : null}
				<Badge variant="muted">{formatDuration(durationSeconds)}</Badge>
				<Badge variant="muted">{formatBytes(sizeBytes)}</Badge>
				{showParts ? (
					<Badge variant="muted">
						{t("videos.parts_count", { count: partCount })}
					</Badge>
				) : null}
				<Badge variant="muted">
					{t("videos.recorded")} {new Date(recordedDate).toLocaleDateString()}
				</Badge>
			</div>

			<div className="mt-5 flex items-center gap-4 border-y border-foreground/10 py-4">
				<Link
					to="/dashboard/channels/$channelId"
					params={{ channelId: video.broadcaster_id }}
					className="flex flex-1 items-center gap-3 min-w-0 hover:text-link transition-colors duration-75"
				>
					<Avatar
						src={channel?.profile_image_url}
						name={channelLabel}
						alt={channelLabel}
						size="lg"
						isLive={isLive}
						liveRingClass="ring-background"
					/>
					<div className="flex flex-col min-w-0">
						<span className="font-medium truncate">{channelLabel}</span>
						{channelStatLine.length > 0 ? (
							<span className="text-xs text-muted-foreground truncate">
								{channelStatLine.join(" · ")}
							</span>
						) : channel?.broadcaster_login &&
							channel.broadcaster_login !== channelLabel.toLowerCase() ? (
							<span className="text-xs text-muted-foreground truncate">
								@{channel.broadcaster_login}
							</span>
						) : null}
					</div>
				</Link>
				{canDownload && (
					<TriggerDownloadDialog
						broadcasterId={video.broadcaster_id}
						broadcasterName={channelLabel}
					>
						<Button variant="outline" size="sm" className="shrink-0">
							<DownloadIcon weight="regular" />
							{t("watch.record_live")}
						</Button>
					</TriggerDownloadDialog>
				)}
			</div>
		</div>
	);
}
