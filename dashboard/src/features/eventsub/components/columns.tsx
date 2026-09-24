import type { ColumnDef } from "@tanstack/react-table";
import type { TFunction } from "i18next";
import { Button } from "@/components/ui/button";
import { Skeleton } from "@/components/ui/skeleton";
import { useUnsubscribe } from "@/features/eventsub";
import { cn } from "@/lib/utils";

export type SubRowData = {
	id: string;
	type: string;
	version: string;
	status: string;
	cost: number;
	broadcaster_id?: string;
};

function CellLineSkeleton({ className }: { className: string }) {
	return (
		<div className="flex h-5 items-center">
			<Skeleton className={cn("h-3.5", className)} />
		</div>
	);
}

function UnsubButton({ id, label }: { id: string; label: string }) {
	const unsub = useUnsubscribe();
	return (
		<Button
			onClick={() => unsub.mutate({ id, reason: "manual" })}
			disabled={unsub.isPending}
			variant="link-destructive"
			size="inline"
			className="text-xs"
		>
			{label}
		</Button>
	);
}

export function subscriptionColumns(t: TFunction): ColumnDef<SubRowData>[] {
	return [
		{
			accessorKey: "type",
			header: t("eventsub.col_type"),
			enableSorting: true,
			meta: { skeleton: <CellLineSkeleton className="w-32" /> },
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
			meta: { skeleton: <CellLineSkeleton className="w-20" /> },
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
			meta: { skeleton: <CellLineSkeleton className="w-16" /> },
		},
		{
			accessorKey: "cost",
			header: () => (
				<span className="text-right w-full">{t("eventsub.col_cost")}</span>
			),
			enableSorting: true,
			meta: { skeleton: <CellLineSkeleton className="ml-auto w-4" /> },
			cell: ({ row }) => <div className="text-right">{row.original.cost}</div>,
		},
		{
			id: "actions",
			header: () => <span className="sr-only">{t("common.actions")}</span>,
			enableSorting: false,
			meta: { skeleton: <CellLineSkeleton className="ml-auto w-20" /> },
			cell: ({ row }) => (
				<div className="text-right">
					<UnsubButton id={row.original.id} label={t("eventsub.unsubscribe")} />
				</div>
			),
		},
	];
}
