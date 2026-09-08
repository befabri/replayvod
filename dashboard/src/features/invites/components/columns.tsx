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

// Row buttons mount their own mutation hook so each row's pending state
// stays isolated.
function RotateButton({
	id,
	onIssued,
	t,
}: {
	id: number;
	onIssued: InviteActions["onIssued"];
	t: TFunction;
}) {
	const rotate = useRotateInvite();
	return (
		<button
			type="button"
			disabled={rotate.isPending}
			onClick={() =>
				rotate.mutate(
					{ id },
					{
						onSuccess: (info) => onIssued(info),
						onError: () => toast.error(t("invites.failed_to_rotate")),
					},
				)
			}
			className="text-link hover:underline disabled:opacity-60"
		>
			{t("invites.new_link")}
		</button>
	);
}

function RevokeButton({
	id,
	onRevoked,
	t,
}: {
	id: number;
	onRevoked: InviteActions["onRevoked"];
	t: TFunction;
}) {
	const revoke = useRevokeInvite();
	return (
		<button
			type="button"
			disabled={revoke.isPending}
			onClick={() => revoke.mutate({ id }, { onSuccess: () => onRevoked(id) })}
			className="text-destructive hover:underline disabled:opacity-60"
		>
			{t("invites.revoke")}
		</button>
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
			cell: ({ row }) => {
				const status = inviteStatus(row.original);
				return (
					<div className="flex justify-end gap-3 whitespace-nowrap">
						{status === "pending" && (
							<RotateButton
								id={row.original.id}
								onIssued={actions.onIssued}
								t={t}
							/>
						)}
						{status !== "redeemed" && (
							<RevokeButton
								id={row.original.id}
								onRevoked={actions.onRevoked}
								t={t}
							/>
						)}
					</div>
				);
			},
		},
	];
}
