import type { ColumnDef } from "@tanstack/react-table";
import type { VideoResponse } from "@/api/generated/trpc";
import { Avatar } from "@/components/ui/avatar";
import { QualityTag } from "@/components/ui/quality-tag";
import { formatBytes, formatDuration } from "@/features/videos/format";
import { channelLabel } from "@/features/videos/labels";

export const sampleVideoColumns: ColumnDef<VideoResponse>[] = [
	{
		id: "channel",
		accessorFn: (video) => channelLabel(video),
		header: "Channel",
		cell: ({ row }) => (
			<span className="flex items-center gap-2 font-medium">
				<Avatar
					src={row.original.profile_image_url}
					name={channelLabel(row.original)}
					size="sm"
				/>
				{channelLabel(row.original)}
			</span>
		),
	},
	{
		accessorKey: "title",
		header: "Title",
		enableSorting: false,
		cell: ({ row }) => (
			<span className="block max-w-xs truncate">{row.original.title}</span>
		),
	},
	{
		accessorKey: "quality",
		header: "Quality",
		enableSorting: false,
		cell: ({ row }) => <QualityTag>{row.original.quality}</QualityTag>,
	},
	{
		accessorKey: "duration_seconds",
		header: "Duration",
		cell: ({ row }) => (
			<span className="font-mono text-sm tabular-nums">
				{formatDuration(row.original.duration_seconds)}
			</span>
		),
	},
	{
		accessorKey: "size_bytes",
		header: "Size",
		cell: ({ row }) => (
			<span className="tabular-nums">
				{formatBytes(row.original.size_bytes)}
			</span>
		),
	},
	{
		accessorKey: "start_download_at",
		header: "Recorded",
		cell: ({ row }) => (
			<span className="whitespace-nowrap text-sm tabular-nums">
				{new Date(row.original.start_download_at).toLocaleString("en", {
					dateStyle: "short",
					timeStyle: "short",
					timeZone: "UTC",
				})}
			</span>
		),
	},
];
