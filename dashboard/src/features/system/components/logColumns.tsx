import type { ColumnDef } from "@tanstack/react-table"
import type { FetchLogEntry } from "@/features/system"

export const fetchLogColumns: ColumnDef<FetchLogEntry>[] = [
	{
		accessorKey: "fetched_at",
		header: "Time",
		enableSorting: true,
		cell: ({ row }) => (
			<span className="whitespace-nowrap text-muted-foreground">
				{new Date(row.original.fetched_at).toLocaleString()}
			</span>
		),
	},
	{
		accessorKey: "fetch_type",
		header: "Type",
		enableSorting: true,
	},
	{
		accessorKey: "broadcaster_id",
		header: "Broadcaster",
		enableSorting: true,
		cell: ({ row }) => (
			<span className="text-muted-foreground">
				{row.original.broadcaster_id || "—"}
			</span>
		),
	},
	{
		accessorKey: "status",
		header: "Status",
		enableSorting: true,
		cell: ({ row }) => {
			const ok = row.original.status >= 200 && row.original.status < 300
			return (
				<span
					className={
						ok
							? "text-emerald-600 dark:text-emerald-400"
							: "text-destructive"
					}
				>
					{row.original.status}
				</span>
			)
		},
	},
	{
		accessorKey: "duration_ms",
		header: "Duration",
		enableSorting: true,
		cell: ({ row }) => (
			<span className="text-muted-foreground">
				{row.original.duration_ms}ms
			</span>
		),
	},
	{
		accessorKey: "error",
		header: "Error",
		enableSorting: false,
		cell: ({ row }) => (
			<span className="text-destructive truncate max-w-xs inline-block">
				{row.original.error || ""}
			</span>
		),
	},
]
