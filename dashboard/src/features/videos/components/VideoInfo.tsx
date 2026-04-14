import { Link } from "@tanstack/react-router";
import { useTranslation } from "react-i18next";
import { Avatar } from "@/components/ui/avatar";
import { Badge } from "@/components/ui/badge";
import { useChannel } from "@/features/channels";
import type { VideoResponse } from "@/features/videos";
import { formatBytes, formatDuration } from "@/features/videos/format";

export function VideoInfo({ video }: { video: VideoResponse }) {
	const { t } = useTranslation();
	// VideoResponse only has broadcaster_id — fetch the channel for
	// display name + avatar so we don't surface raw IDs in the UI.
	const { data: channel } = useChannel(video.broadcaster_id);

	const channelLabel = channel?.broadcaster_name ?? video.broadcaster_id;
	const titleLabel = video.title?.trim() || video.display_name;

	return (
		<div className="flex flex-col gap-3">
			<h2 className="text-2xl md:text-3xl font-heading font-semibold tracking-tight">
				{titleLabel}
			</h2>
			<div className="flex flex-wrap items-center gap-3 text-sm">
				<Link
					// biome-ignore lint/suspicious/noExplicitAny: dynamic param route
					to={"/dashboard/channels/$channelId" as any}
					// biome-ignore lint/suspicious/noExplicitAny: dynamic param route
					params={{ channelId: video.broadcaster_id } as any}
					className="flex items-center gap-2 hover:text-link transition-colors duration-75"
				>
					<Avatar
						src={channel?.profile_image_url}
						name={channelLabel}
						alt={channelLabel}
						size="sm"
					/>
					<span className="font-medium">{channelLabel}</span>
				</Link>
				<Badge variant="blue">{video.quality}</Badge>
				{video.language ? (
					<Badge variant="muted">{video.language}</Badge>
				) : null}
				<span className="text-muted-foreground">
					{formatDuration(video.duration_seconds)}
				</span>
				<span className="text-muted-foreground">·</span>
				<span className="text-muted-foreground">
					{formatBytes(video.size_bytes)}
				</span>
				{video.downloaded_at ? (
					<>
						<span className="text-muted-foreground">·</span>
						<span className="text-muted-foreground">
							{t("videos.status.DONE")}{" "}
							{new Date(video.downloaded_at).toLocaleDateString()}
						</span>
					</>
				) : null}
			</div>

		</div>
	);
}
