import type { ColumnDef } from "@tanstack/react-table"
import { Link } from "@tanstack/react-router"
import type { TFunction } from "i18next"
import type { VideoSummary } from "@/api/generated/trpc"

export function requestColumns(t: TFunction): ColumnDef<VideoSummary>[] {
	return [
		{
			accessorKey: "display_name",
			header: "Title",
			enableSorting: true,
		},
		{
			accessorKey: "status",
			header: "Status",
			enableSorting: true,
			cell: ({ row }) =>
				t(`videos.status.${row.original.status}` as const, row.original.status),
		},
		{
			id: "actions",
			header: () => <span className="text-right w-full block">Actions</span>,
			enableSorting: false,
			cell: ({ row }) => (
				<div className="text-right">
					{row.original.status === "DONE" && (
						<Link
							// biome-ignore lint/suspicious/noExplicitAny: param route typing
							to={"/dashboard/watch/$videoId" as any}
							params={{ videoId: String(row.original.id) } as any}
							className="text-primary hover:underline"
						>
							Watch
						</Link>
					)}
				</div>
			),
		},
	]
}
