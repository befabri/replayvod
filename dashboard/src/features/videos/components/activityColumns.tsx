import type { ColumnDef } from "@tanstack/react-table";
import type { VideoResponse } from "@/features/videos";
import { formatBytes, formatDuration } from "@/features/videos/format";
import { useCancelDownload } from "@/features/videos/queries";
import { VideoStatusBadge } from "./VideoStatusBadge";

function CancelButton({ jobId }: { jobId: string }) {
	const cancel = useCancelDownload();
	return (
		<button
			type="button"
			onClick={() => cancel.mutate({ job_id: jobId })}
			disabled={cancel.isPending}
			className="text-xs text-destructive hover:underline disabled:opacity-60"
		>
			Cancel
		</button>
	);
}

export const queueColumns: ColumnDef<VideoResponse>[] = [
	{
		accessorKey: "display_name",
		header: "Channel",
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
		cell: ({ row }) => (
			<div className="text-right">
				{row.original.status !== "DONE" && row.original.status !== "FAILED" && (
					<CancelButton jobId={row.original.job_id} />
				)}
			</div>
		),
	},
];

export const historyColumns: ColumnDef<VideoResponse>[] = [
	{
		accessorKey: "display_name",
		header: "Channel",
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
		accessorKey: "downloaded_at",
		header: "Finished",
		enableSorting: true,
		cell: ({ row }) => (
			<span className="text-xs text-muted-foreground">
				{row.original.downloaded_at
					? new Date(row.original.downloaded_at).toLocaleString()
					: "—"}
			</span>
		),
	},
];
