import type { ColumnDef } from "@tanstack/react-table"
import type { SessionInfo } from "@/features/sessions"
import { useRevokeSession } from "@/features/sessions"

function RevokeButton({
	hashedId,
	isCurrent,
}: {
	hashedId: string
	isCurrent: boolean
}) {
	const revoke = useRevokeSession()
	return (
		<button
			type="button"
			disabled={revoke.isPending}
			onClick={() => revoke.mutate({ hashed_id: hashedId })}
			className="text-destructive hover:underline text-xs disabled:opacity-60"
		>
			{isCurrent ? "Sign out" : "Revoke"}
		</button>
	)
}

export const sessionColumns: ColumnDef<SessionInfo>[] = [
	{
		accessorKey: "user_agent",
		header: "Device",
		enableSorting: false,
		cell: ({ row }) => (
			<div className="flex items-center gap-2">
				<span className="text-sm truncate max-w-sm" title={row.original.user_agent}>
					{row.original.user_agent || "Unknown"}
				</span>
				{row.original.current && (
					<span className="text-xs px-1.5 py-0.5 rounded bg-primary/20 text-primary-foreground">
						current
					</span>
				)}
			</div>
		),
	},
	{
		accessorKey: "ip_address",
		header: "IP",
		enableSorting: true,
		cell: ({ row }) => (
			<span className="font-mono text-xs text-muted-foreground">
				{row.original.ip_address || "—"}
			</span>
		),
	},
	{
		accessorKey: "last_active_at",
		header: "Last active",
		enableSorting: true,
		cell: ({ row }) => (
			<span className="text-xs text-muted-foreground">
				{new Date(row.original.last_active_at).toLocaleString()}
			</span>
		),
	},
	{
		accessorKey: "expires_at",
		header: "Expires",
		enableSorting: true,
		cell: ({ row }) => (
			<span className="text-xs text-muted-foreground">
				{new Date(row.original.expires_at).toLocaleString()}
			</span>
		),
	},
	{
		id: "actions",
		header: "",
		enableSorting: false,
		cell: ({ row }) => (
			<div className="text-right">
				<RevokeButton
					hashedId={row.original.hashed_id}
					isCurrent={row.original.current}
				/>
			</div>
		),
	},
]
