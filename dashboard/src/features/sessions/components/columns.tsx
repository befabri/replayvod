import type { ColumnDef } from "@tanstack/react-table";
import type { TFunction } from "i18next";
import type { SessionInfo } from "@/features/sessions";
import { useRevokeSession } from "@/features/sessions";

function RevokeButton({
	hashedId,
	isCurrent,
	t,
}: {
	hashedId: string;
	isCurrent: boolean;
	t: TFunction;
}) {
	const revoke = useRevokeSession();
	return (
		<button
			type="button"
			disabled={revoke.isPending}
			onClick={() => revoke.mutate({ hashed_id: hashedId })}
			className="text-destructive hover:underline text-xs disabled:opacity-60"
		>
			{isCurrent ? t("sessions.sign_out") : t("sessions.revoke")}
		</button>
	);
}

export function sessionColumns(t: TFunction): ColumnDef<SessionInfo>[] {
	return [
		{
			accessorKey: "user_agent",
			header: t("sessions.col_device"),
			enableSorting: false,
			cell: ({ row }) => (
				<div className="flex items-center gap-2">
					<span
						className="text-sm truncate max-w-sm"
						title={row.original.user_agent}
					>
						{row.original.user_agent || t("sessions.unknown_device")}
					</span>
					{row.original.current && (
						<span className="text-xs px-1.5 py-0.5 rounded bg-primary/20 text-foreground">
							{t("sessions.current")}
						</span>
					)}
				</div>
			),
		},
		{
			accessorKey: "ip_address",
			header: t("sessions.col_ip"),
			enableSorting: true,
			cell: ({ row }) => (
				<span className="font-mono text-xs text-muted-foreground">
					{row.original.ip_address || "—"}
				</span>
			),
		},
		{
			accessorKey: "last_active_at",
			header: t("sessions.col_last_active"),
			enableSorting: true,
			cell: ({ row }) => (
				<span className="text-xs text-muted-foreground">
					{new Date(row.original.last_active_at).toLocaleString()}
				</span>
			),
		},
		{
			accessorKey: "expires_at",
			header: t("sessions.col_expires"),
			enableSorting: true,
			cell: ({ row }) => (
				<span className="text-xs text-muted-foreground">
					{new Date(row.original.expires_at).toLocaleString()}
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
					<RevokeButton
						hashedId={row.original.hashed_id}
						isCurrent={row.original.current}
						t={t}
					/>
				</div>
			),
		},
	];
}
