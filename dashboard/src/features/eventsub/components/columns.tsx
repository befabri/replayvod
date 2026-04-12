import type { ColumnDef } from "@tanstack/react-table"
import type { TFunction } from "i18next"
import { useUnsubscribe } from "@/features/eventsub"

// Row shape the subscriptions query actually returns; condition is
// json.RawMessage server-side so broadcaster_id may be absent.
export type SubRowData = {
	id: string
	type: string
	version: string
	status: string
	cost: number
	broadcaster_id?: string
}

function UnsubButton({ id, label }: { id: string; label: string }) {
	const unsub = useUnsubscribe()
	return (
		<button
			type="button"
			onClick={() => unsub.mutate({ id, reason: "manual" })}
			disabled={unsub.isPending}
			className="text-destructive hover:underline text-xs disabled:opacity-60"
		>
			{label}
		</button>
	)
}

export function subscriptionColumns(t: TFunction): ColumnDef<SubRowData>[] {
	return [
		{
			accessorKey: "type",
			header: t("eventsub.col_type"),
			enableSorting: true,
			cell: ({ row }) => (
				<span className="font-mono text-xs">
					{row.original.type}{" "}
					<span className="text-muted-foreground">v{row.original.version}</span>
				</span>
			),
		},
		{
			accessorKey: "broadcaster_id",
			header: t("eventsub.col_broadcaster"),
			enableSorting: true,
			cell: ({ row }) => (
				<span className="font-mono text-xs">
					{row.original.broadcaster_id ?? "—"}
				</span>
			),
		},
		{
			accessorKey: "status",
			header: t("eventsub.col_status"),
			enableSorting: true,
		},
		{
			accessorKey: "cost",
			header: () => <span className="text-right w-full">{t("eventsub.col_cost")}</span>,
			enableSorting: true,
			cell: ({ row }) => <div className="text-right">{row.original.cost}</div>,
		},
		{
			id: "actions",
			header: "",
			enableSorting: false,
			cell: ({ row }) => (
				<div className="text-right">
					<UnsubButton id={row.original.id} label={t("eventsub.unsubscribe")} />
				</div>
			),
		},
	]
}
