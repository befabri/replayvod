import type { ColumnDef } from "@tanstack/react-table";
import type { TFunction } from "i18next";
import type { VideoResponse } from "@/features/videos";
import { formatBytes, formatDuration } from "@/features/videos/format";
import { useCancelDownload } from "@/features/videos/queries";
import { VideoStatusBadge } from "./VideoStatusBadge";

function CancelButton({ jobId, t }: { jobId: string; t: TFunction }) {
	const cancel = useCancelDownload();
	return (
		<button
			type="button"
			onClick={() => cancel.mutate({ job_id: jobId })}
			disabled={cancel.isPending}
			className="text-xs text-destructive hover:underline disabled:opacity-60"
		>
			{t("queue.cancel")}
		</button>
	);
}

export function queueColumns(t: TFunction): ColumnDef<VideoResponse>[] {
	return [
		{
			accessorKey: "display_name",
			header: t("queue.col_channel"),
			enableSorting: true,
			cell: ({ row }) => (
				<span className="font-medium">{row.original.display_name}</span>
			),
		},
		{
			accessorKey: "status",
			header: t("queue.col_status"),
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
			header: t("queue.col_quality"),
			enableSorting: true,
		},
		{
			accessorKey: "start_download_at",
			header: t("queue.col_started"),
			enableSorting: true,
			cell: ({ row }) => (
				<span className="text-xs text-muted-foreground">
					{new Date(row.original.start_download_at).toLocaleString()}
				</span>
			),
		},
		{
			id: "actions",
			header: () => (
				<span className="text-right w-full block">{t("common.actions")}</span>
			),
			enableSorting: false,
			cell: ({ row }) => (
				<div className="text-right">
					{row.original.status !== "DONE" &&
						row.original.status !== "FAILED" && (
							<CancelButton jobId={row.original.job_id} t={t} />
						)}
				</div>
			),
		},
	];
}

export function historyColumns(t: TFunction): ColumnDef<VideoResponse>[] {
	return [
		{
			accessorKey: "display_name",
			header: t("history.col_channel"),
			enableSorting: true,
			cell: ({ row }) => (
				<span className="font-medium">{row.original.display_name}</span>
			),
		},
		{
			accessorKey: "status",
			header: t("history.col_status"),
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
			header: t("history.col_quality"),
			enableSorting: true,
		},
		{
			accessorKey: "duration_seconds",
			header: t("history.col_duration"),
			enableSorting: true,
			cell: ({ row }) => (
				<span className="text-xs text-muted-foreground">
					{formatDuration(row.original.duration_seconds)}
				</span>
			),
		},
		{
			accessorKey: "size_bytes",
			header: t("history.col_size"),
			enableSorting: true,
			cell: ({ row }) => (
				<span className="text-xs text-muted-foreground">
					{formatBytes(row.original.size_bytes)}
				</span>
			),
		},
		{
			accessorKey: "downloaded_at",
			header: t("history.col_finished"),
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
}
