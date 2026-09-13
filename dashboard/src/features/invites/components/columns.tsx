import type { ColumnDef } from "@tanstack/react-table";
import type { TFunction } from "i18next";
import { toast } from "sonner";
import { Badge } from "@/components/ui/badge";
import type {
	InviteCreatedInfo,
	InviteInfo,
	InviteStatus,
} from "@/features/invites";
import {
	inviteStatus,
	useRevokeInvite,
	useRotateInvite,
} from "@/features/invites";

type InviteStatusLabelKey =
	| "invites.status_pending"
	| "invites.status_redeemed"
	| "invites.status_expired";
const STATUS_LABEL_KEYS: Record<InviteStatus, InviteStatusLabelKey> = {
	pending: "invites.status_pending",
	redeemed: "invites.status_redeemed",
	expired: "invites.status_expired",
};
const STATUS_VARIANTS: Record<InviteStatus, "default" | "emerald" | "muted"> = {
	pending: "default",
	redeemed: "emerald",
	expired: "muted",
};

// InviteActions is how row actions reach the section that owns the
// copy-once panel: a freshly issued link to show, or a revoked invite
// whose link must stop being offered.
export type InviteActions = {
	onIssued: (info: InviteCreatedInfo) => void;
	onRevoked: (id: number) => void;
};

// Keep rotation and revocation mutually exclusive so a late rotation response
// cannot put a revoked link back into the copy panel.
function InviteRowActions({
	invite,
	actions,
	t,
}: {
	invite: InviteInfo;
	actions: InviteActions;
	t: TFunction;
}) {
	const rotate = useRotateInvite();
	const revoke = useRevokeInvite();
	const pending = rotate.isPending || revoke.isPending;
	const status = inviteStatus(invite);
	return (
		<div className="flex justify-end gap-3 whitespace-nowrap">
			{status === "pending" && (
				<button
					type="button"
					disabled={pending}
					onClick={() =>
						rotate.mutate(
							{ id: invite.id },
							{
								onSuccess: actions.onIssued,
								onError: () => toast.error(t("invites.failed_to_rotate")),
							},
						)
					}
					className="text-link hover:underline disabled:opacity-60"
				>
					{t("invites.new_link")}
				</button>
			)}
			{status !== "redeemed" && (
				<button
					type="button"
					disabled={pending}
					onClick={() =>
						revoke.mutate(
							{ id: invite.id },
							{ onSuccess: () => actions.onRevoked(invite.id) },
						)
					}
					className="text-destructive hover:underline disabled:opacity-60"
				>
					{t("invites.revoke")}
				</button>
			)}
		</div>
	);
}

export function inviteColumns(
	t: TFunction,
	actions: InviteActions,
): ColumnDef<InviteInfo>[] {
	return [
		{
			accessorKey: "note",
			header: t("invites.col_note"),
			enableSorting: true,
			cell: ({ row }) =>
				row.original.note ? (
					<span>{row.original.note}</span>
				) : (
					<span className="text-muted-foreground">—</span>
				),
		},
		{
			accessorKey: "role",
			header: t("invites.col_role"),
			enableSorting: true,
			cell: ({ row }) => (
				<span className="text-muted-foreground">{row.original.role}</span>
			),
		},
		{
			id: "status",
			header: t("invites.col_status"),
			enableSorting: false,
			cell: ({ row }) => {
				const status = inviteStatus(row.original);
				return (
					<Badge variant={STATUS_VARIANTS[status]}>
						{t(STATUS_LABEL_KEYS[status])}
					</Badge>
				);
			},
		},
		{
			accessorKey: "expires_at",
			header: t("invites.col_expires"),
			enableSorting: true,
			cell: ({ row }) => (
				<span className="text-muted-foreground">
					{new Date(row.original.expires_at).toLocaleString()}
				</span>
			),
		},
		{
			accessorKey: "redeemed_by",
			header: t("invites.col_redeemed_by"),
			enableSorting: false,
			cell: ({ row }) =>
				row.original.redeemed_by ? (
					<span className="font-mono">{row.original.redeemed_by}</span>
				) : (
					<span className="text-muted-foreground">—</span>
				),
		},
		{
			id: "actions",
			header: () => (
				<span className="text-right w-full block">{t("common.actions")}</span>
			),
			enableSorting: false,
			cell: ({ row }) => (
				<InviteRowActions invite={row.original} actions={actions} t={t} />
			),
		},
	];
}
