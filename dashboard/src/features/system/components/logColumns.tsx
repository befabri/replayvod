import type { ColumnDef } from "@tanstack/react-table";
import type { TFunction } from "i18next";
import type { FetchLogEntry } from "@/features/system";

export function fetchLogColumns(t: TFunction): ColumnDef<FetchLogEntry>[] {
	return [
		{
			accessorKey: "fetched_at",
			header: t("logs.col_time"),
			enableSorting: true,
			cell: ({ row }) => (
				<span className="whitespace-nowrap text-muted-foreground">
					{new Date(row.original.fetched_at).toLocaleString()}
				</span>
			),
		},
		{
			accessorKey: "fetch_type",
			header: t("logs.col_type"),
			enableSorting: true,
		},
		{
			accessorKey: "broadcaster_id",
			header: t("logs.col_broadcaster"),
			enableSorting: true,
			cell: ({ row }) => (
				<span className="text-muted-foreground">
					{row.original.broadcaster_id || "—"}
				</span>
			),
		},
		{
			accessorKey: "status",
			header: t("logs.col_status"),
			enableSorting: true,
			cell: ({ row }) => {
				const ok = row.original.status >= 200 && row.original.status < 300;
				return (
					<span className={ok ? "text-badge-green-fg" : "text-destructive"}>
						{row.original.status}
					</span>
				);
			},
		},
		{
			accessorKey: "duration_ms",
			header: t("logs.col_duration"),
			enableSorting: true,
			cell: ({ row }) => (
				<span className="text-muted-foreground">
					{row.original.duration_ms}ms
				</span>
			),
		},
		{
			accessorKey: "error",
			header: t("logs.col_error"),
			enableSorting: false,
			cell: ({ row }) => (
				<span className="text-destructive truncate max-w-xs inline-block">
					{row.original.error || ""}
				</span>
			),
		},
	];
}
