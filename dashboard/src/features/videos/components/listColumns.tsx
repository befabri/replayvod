import { ImageBroken } from "@phosphor-icons/react";
import { Link } from "@tanstack/react-router";
import type { ColumnDef } from "@tanstack/react-table";
import type { TFunction } from "i18next";
import { Avatar } from "@/components/ui/avatar";
import { Timestamp } from "@/components/ui/timestamp";
import { API_URL } from "@/env";
import { channelLabel, type VideoResponse } from "@/features/videos";
import { formatBytes, formatDuration } from "@/features/videos/format";
import { VideoStatusBadge } from "./VideoStatusBadge";

export function videoListColumns(t: TFunction): ColumnDef<VideoResponse>[] {
	return [
		{
			id: "thumbnail",
			header: "Thumb",
			enableSorting: false,
			cell: ({ row }) => <VideoThumbnail video={row.original} t={t} />,
		},
		{
			accessorKey: "display_name",
			header: "Title",
			enableSorting: true,
			cell: ({ row }) => <VideoTitleCell row={row.original} t={t} />,
		},
		{
			id: "channel",
			accessorFn: (row) => channelLabel(row),
			header: "Channel",
			enableSorting: true,
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
				<Timestamp
					iso={row.original.start_download_at}
					className="text-xs text-muted-foreground"
				/>
			),
		},
		{
			id: "actions",
			header: "",
			enableSorting: false,
			cell: ({ row }) =>
				row.original.status === "DONE" ? (
					<Link
						to="/dashboard/watch/$videoId"
						params={{ videoId: String(row.original.id) }}
						className="text-primary text-xs hover:underline"
					>
						{t("videos.watch")}
					</Link>
				) : null,
		},
	];
}

function VideoThumbnail({ video, t }: { video: VideoResponse; t: TFunction }) {
	const thumbnail = video.thumbnail
		? `${API_URL}/api/v1/thumbnails/${video.thumbnail.replace(/^thumbnails\//, "")}`
		: null;

	return (
		<div className="relative h-16 w-28 overflow-hidden rounded-md bg-muted">
			{thumbnail ? (
				<img
					src={thumbnail}
					alt=""
					className="h-full w-full object-cover"
					loading="lazy"
				/>
			) : (
				<div
					className="flex h-full items-center justify-center text-muted-foreground/60"
					role="img"
					aria-label={t("videos.no_thumbnail")}
				>
					<ImageBroken className="size-5" />
				</div>
			)}
			<span className="absolute right-1 bottom-1 rounded bg-background/90 px-1.5 py-0.5 text-[10px] font-medium text-foreground">
				{formatDuration(video.duration_seconds)}
			</span>
		</div>
	);
}

function VideoTitleCell({ row, t }: { row: VideoResponse; t: TFunction }) {
	const label = row.title?.trim() || row.display_name;
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
			className="block hover:text-link"
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
