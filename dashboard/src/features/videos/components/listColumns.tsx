import type { ColumnDef } from "@tanstack/react-table"
import { Link } from "@tanstack/react-router"
import type { TFunction } from "i18next"
import type { VideoResponse } from "@/features/videos"
import { formatBytes, formatDuration } from "@/features/videos/format"
import { VideoStatusBadge } from "./VideoStatusBadge"

export function videoListColumns(t: TFunction): ColumnDef<VideoResponse>[] {
	return [
		{
			accessorKey: "display_name",
			header: "Title",
			enableSorting: true,
			cell: ({ row }) => (
				<span className="font-medium">{row.original.display_name}</span>
			),
		},
		{
			accessorKey: "status",
			header: "Status",
			enableSorting: true,
			cell: ({ row }) => <VideoStatusBadge status={row.original.status} />,
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
				<span className="text-xs text-muted-foreground">
					{new Date(row.original.start_download_at).toLocaleString()}
				</span>
			),
		},
		{
			id: "actions",
			header: "",
			enableSorting: false,
			cell: ({ row }) =>
				row.original.status === "DONE" ? (
					<Link
						// biome-ignore lint/suspicious/noExplicitAny: param route typing
						to={"/dashboard/watch/$videoId" as any}
						params={{ videoId: String(row.original.id) } as any}
						className="text-primary text-xs hover:underline"
					>
						{t("videos.watch")}
					</Link>
				) : null,
		},
	]
}
