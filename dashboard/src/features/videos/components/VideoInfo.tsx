import { Download } from "@phosphor-icons/react";
import { Link } from "@tanstack/react-router";
import { useTranslation } from "react-i18next";
import { Avatar } from "@/components/ui/avatar";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { useChannel } from "@/features/channels";
import { useLastLive } from "@/features/streams-live";
import type { VideoResponse } from "@/features/videos";
import { formatBytes, formatDuration } from "@/features/videos/format";
import {
	useChannelStatistics,
	useVideoCategories,
} from "@/features/videos/queries";
import { useRelativeTime } from "@/lib/format-relative";
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

	const channelLabel = channel?.broadcaster_name ?? video.broadcaster_id;
	const titleLabel = video.title?.trim() || video.display_name;
	const recordedDate = video.downloaded_at ?? video.start_download_at;

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
				<Badge variant="default">{video.quality}</Badge>
				{video.language ? (
					<Badge variant="muted" className="uppercase tracking-wide">
						{video.language}
					</Badge>
				) : null}
				{topCategory ? (
					<Link
						// biome-ignore lint/suspicious/noExplicitAny: param route typing
						to={"/dashboard/categories/$categoryId" as any}
						// biome-ignore lint/suspicious/noExplicitAny: param route typing
						params={{ categoryId: topCategory.id } as any}
						className="rounded-md hover:opacity-80 transition-opacity"
					>
						<Badge variant="muted">{topCategory.name}</Badge>
					</Link>
				) : null}
				<Badge variant="muted">{formatDuration(video.duration_seconds)}</Badge>
				<Badge variant="muted">{formatBytes(video.size_bytes)}</Badge>
				<Badge variant="muted">
					{t("videos.recorded")} {new Date(recordedDate).toLocaleDateString()}
				</Badge>
			</div>

			<div className="mt-5 flex items-center gap-4 border-y border-foreground/10 py-4">
				<Link
					// biome-ignore lint/suspicious/noExplicitAny: dynamic param route
					to={"/dashboard/channels/$channelId" as any}
					// biome-ignore lint/suspicious/noExplicitAny: dynamic param route
					params={{ channelId: video.broadcaster_id } as any}
					className="flex flex-1 items-center gap-3 min-w-0 hover:text-link transition-colors duration-75"
				>
					<Avatar
						src={channel?.profile_image_url}
						name={channelLabel}
						alt={channelLabel}
						size="lg"
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
				<TriggerDownloadDialog
					broadcasterId={video.broadcaster_id}
					broadcasterName={channelLabel}
				>
					<Button variant="outline" size="sm" className="shrink-0">
						<Download weight="regular" />
						{t("watch.record_live")}
					</Button>
				</TriggerDownloadDialog>
			</div>
		</div>
	);
}
