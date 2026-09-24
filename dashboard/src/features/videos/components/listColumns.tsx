import { FilmSlateIcon } from "@phosphor-icons/react";
import { Link } from "@tanstack/react-router";
import type { ColumnDef } from "@tanstack/react-table";
import type { TFunction } from "i18next";
import { useState } from "react";
import { Avatar } from "@/components/ui/avatar";
import { Skeleton } from "@/components/ui/skeleton";
import { TimestampValue } from "@/components/ui/timestamp";
import { channelLabel, type VideoResponse } from "@/features/videos";
import { formatBytes, formatDuration } from "@/features/videos/format";
import { isPlayableVideo } from "@/features/videos/playback";
import { localThumbnailURL } from "@/features/videos/thumbnail";
import { cn } from "@/lib/utils";
import { RemoveVideoButton } from "./RemoveVideoButton";
import { StreamHistoryButton } from "./StreamHistoryButton";
import { VideoStatusBadge } from "./VideoStatusBadge";
import { WatchLaterButton } from "./WatchLaterButton";

export const POSTER_SKELETON = <Skeleton className="h-16 w-28" />;

const TITLE_SKELETON = (
	<div className="space-y-1.5">
		<Skeleton className="h-4 w-48" />
		<Skeleton className="h-3 w-24" />
	</div>
);

const CHANNEL_SKELETON = (
	<div className="flex items-center gap-2.5">
		<Skeleton className="size-6 shrink-0 rounded-full" />
		<div className="space-y-1.5">
			<Skeleton className="h-4 w-24" />
			<Skeleton className="h-3 w-16" />
		</div>
	</div>
);

export function videoListColumns(
	t: TFunction,
	canManage: boolean,
	locale: string,
): ColumnDef<VideoResponse>[] {
	return [
		{
			id: "thumbnail",
			header: "Thumb",
			enableSorting: false,
			meta: { skeleton: POSTER_SKELETON },
			cell: ({ row }) => (
				<PosterCell
					row={row.original}
					label={videoTitle(row.original)}
					watchable={isPlayableVideo(row.original)}
					t={t}
				/>
			),
		},
		{
			accessorKey: "display_name",
			header: "Title",
			enableSorting: true,
			meta: { skeleton: TITLE_SKELETON },
			cell: ({ row }) => <VideoTitleCell row={row.original} t={t} />,
		},
		{
			id: "channel",
			accessorFn: (row) => channelLabel(row),
			header: "Channel",
			enableSorting: true,
			meta: { skeleton: CHANNEL_SKELETON },
			cell: ({ row }) => <VideoChannelCell row={row.original} />,
		},
		{
			accessorKey: "status",
			header: "Status",
			enableSorting: true,
			cell: ({ row }) => (
				<VideoStatusBadge
					status={row.original.status}
					completionKind={row.original.completion_kind}
					t={t}
				/>
			),
		},
		{
			accessorKey: "quality",
			header: "Quality",
			enableSorting: true,
		},
		{
			accessorKey: "duration_seconds",
			header: "Duration",
			enableSorting: true,
			cell: ({ row }) => (
				<span className="text-xs text-muted-foreground">
					{formatDuration(row.original.duration_seconds)}
				</span>
			),
		},
		{
			accessorKey: "size_bytes",
			header: "Size",
			enableSorting: true,
			cell: ({ row }) => (
				<span className="text-xs text-muted-foreground">
					{formatBytes(row.original.size_bytes)}
				</span>
			),
		},
		{
			accessorKey: "start_download_at",
			header: "Started",
			enableSorting: true,
			cell: ({ row }) => (
				<TimestampValue
					iso={row.original.start_download_at}
					locale={locale}
					className="text-xs text-muted-foreground"
				/>
			),
		},
		{
			id: "actions",
			header: "",
			enableSorting: false,
			meta: { skeleton: null },
			cell: ({ row }) => {
				const video = row.original;
				const isDone = video.status === "DONE";
				const canRemove = canManage && (isDone || video.status === "FAILED");
				return (
					<div className="flex items-center justify-end gap-1.5">
						{isDone ? (
							<StreamHistoryButton
								videoId={video.id}
								videoStartDownloadAt={video.start_download_at}
								t={t}
							/>
						) : null}
						<WatchLaterButton
							videoId={video.id}
							watchLater={video.user_state?.watch_later ?? false}
						/>
						{canRemove ? <RemoveVideoButton videoId={video.id} /> : null}
					</div>
				);
			},
		},
	];
}

export function VideoThumbnail({
	video,
	t,
}: {
	video: VideoResponse;
	t: TFunction;
}) {
	const [failedSrc, setFailedSrc] = useState<string | null>(null);
	const thumbnail = video.thumbnail ? localThumbnailURL(video.thumbnail) : null;
	const showImage = thumbnail !== null && thumbnail !== failedSrc;

	return (
		<div className="relative h-16 w-28 overflow-hidden rounded-md bg-muted">
			{showImage ? (
				<img
					src={thumbnail}
					alt=""
					className="h-full w-full object-cover"
					loading="lazy"
					onError={() => setFailedSrc(thumbnail)}
				/>
			) : (
				<div
					className="flex h-full items-center justify-center text-muted-foreground/60"
					role="img"
					aria-label={t("videos.no_thumbnail")}
				>
					<FilmSlateIcon className="size-5" />
				</div>
			)}
			<span className="absolute right-1 bottom-1 rounded bg-background/90 px-1.5 py-0.5 text-[10px] font-medium text-foreground">
				{formatDuration(video.duration_seconds)}
			</span>
		</div>
	);
}

export const FOCUS_RING =
	"focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2";

export function PosterCell({
	row,
	label,
	watchable,
	t,
}: {
	row: VideoResponse;
	label: string;
	watchable: boolean;
	t: TFunction;
}) {
	const poster = <VideoThumbnail video={row} t={t} />;
	if (!watchable) {
		return poster;
	}
	return (
		<Link
			to="/dashboard/watch/$videoId"
			params={{ videoId: String(row.id) }}
			search={{ t: undefined }}
			className={cn("block shrink-0 rounded-md", FOCUS_RING)}
			aria-label={t("videos.watch_recording", { title: label })}
		>
			{poster}
		</Link>
	);
}

function videoTitle(row: VideoResponse) {
	return row.title?.trim() || row.display_name;
}

function VideoTitleCell({ row, t }: { row: VideoResponse; t: TFunction }) {
	const label = videoTitle(row);
	const metaParts = [];
	if (row.language) metaParts.push(row.language.toUpperCase());
	if (row.viewer_count > 0) {
		metaParts.push(t("videos.viewer_count", { count: row.viewer_count }));
	}
	const body = (
		<div className="min-w-0">
			<div className="truncate font-medium" title={label}>
				{label}
			</div>
			<div className="truncate text-xs text-muted-foreground">
				{metaParts.join(" · ") || row.filename}
			</div>
		</div>
	);

	if (row.status !== "DONE") return body;

	return (
		<Link
			to="/dashboard/watch/$videoId"
			params={{ videoId: String(row.id) }}
			search={{ t: undefined }}
			className={cn(
				"block rounded-sm transition-colors hover:text-link",
				FOCUS_RING,
			)}
		>
			{body}
		</Link>
	);
}

function VideoChannelCell({ row }: { row: VideoResponse }) {
	const label = channelLabel(row);
	return (
		<div className="flex min-w-0 items-center gap-2.5">
			<Avatar src={row.profile_image_url} name={label} alt={label} size="sm" />
			<div className="min-w-0">
				<div className="truncate font-medium">{label}</div>
				<div className="truncate text-xs text-muted-foreground">
					{row.broadcaster_login
						? `@${row.broadcaster_login}`
						: row.broadcaster_id}
				</div>
			</div>
		</div>
	);
}
