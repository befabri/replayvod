import type { ColumnDef } from "@tanstack/react-table";
import type { TFunction } from "i18next";
import type { EventLogEntry } from "@/features/eventlogs";
import { SeverityBadge } from "./SeverityBadge";

export function eventLogColumns(t: TFunction): ColumnDef<EventLogEntry>[] {
	return [
		{
			accessorKey: "created_at",
			header: t("events.col_time"),
			enableSorting: true,
			cell: ({ row }) => (
				<span className="text-xs text-muted-foreground">
					{new Date(row.original.created_at).toLocaleString()}
				</span>
			),
		},
		{
			accessorKey: "severity",
			header: t("events.col_severity"),
			enableSorting: true,
			cell: ({ row }) => <SeverityBadge severity={row.original.severity} />,
		},
		{
			id: "domain",
			header: t("events.col_domain"),
			enableSorting: false,
			cell: ({ row }) => (
				<span className="font-mono text-xs">
					{row.original.domain}.{row.original.event_type}
				</span>
			),
		},
		{
			id: "message",
			header: t("events.col_message"),
			enableSorting: false,
			cell: ({ row }) => (
				<div>
					<div>{row.original.message}</div>
					{row.original.data !== undefined && row.original.data !== null && (
						<pre className="text-xs text-muted-foreground mt-1 overflow-x-auto">
							{JSON.stringify(row.original.data as unknown, null, 2)}
						</pre>
					)}
				</div>
			),
		},
	];
}
