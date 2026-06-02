import { Link } from "@tanstack/react-router";
import type { ColumnDef } from "@tanstack/react-table";
import type { TFunction } from "i18next";
import type { VideoSummary } from "@/api/generated/trpc";
import { videoStatusLabel } from "@/features/videos/labels";

export function requestColumns(t: TFunction): ColumnDef<VideoSummary>[] {
	return [
		{
			accessorKey: "display_name",
			header: t("requests.col_title"),
			enableSorting: true,
		},
		{
			accessorKey: "status",
			header: t("requests.col_status"),
			enableSorting: true,
			cell: ({ row }) => videoStatusLabel(t, row.original.status),
		},
		{
			id: "actions",
			header: () => (
				<span className="text-right w-full block">{t("common.actions")}</span>
			),
			enableSorting: false,
			cell: ({ row }) => (
				<div className="text-right">
					{row.original.status === "DONE" && (
						<Link
							to="/dashboard/watch/$videoId"
							params={{ videoId: String(row.original.id) }}
							search={{ t: undefined }}
							className="text-primary hover:underline"
						>
							{t("requests.watch")}
						</Link>
					)}
				</div>
			),
		},
	];
}
