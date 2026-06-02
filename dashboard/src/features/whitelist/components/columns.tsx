import type { ColumnDef } from "@tanstack/react-table";
import type { TFunction } from "i18next";
import type { WhitelistEntryInfo } from "@/features/whitelist";
import { useRemoveWhitelist } from "@/features/whitelist";

function RemoveButton({
	twitchUserId,
	t,
}: {
	twitchUserId: string;
	t: TFunction;
}) {
	const remove = useRemoveWhitelist();
	return (
		<button
			type="button"
			disabled={remove.isPending}
			onClick={() => remove.mutate({ twitch_user_id: twitchUserId })}
			className="text-destructive hover:underline disabled:opacity-60"
		>
			{t("whitelist.remove")}
		</button>
	);
}

export function whitelistColumns(
	t: TFunction,
): ColumnDef<WhitelistEntryInfo>[] {
	return [
		{
			accessorKey: "twitch_user_id",
			header: t("whitelist.col_twitch_user_id"),
			enableSorting: true,
			cell: ({ row }) => (
				<span className="font-mono">{row.original.twitch_user_id}</span>
			),
		},
		{
			accessorKey: "added_at",
			header: t("whitelist.col_added"),
			enableSorting: true,
			cell: ({ row }) => (
				<span className="text-muted-foreground">
					{new Date(row.original.added_at).toLocaleString()}
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
					<RemoveButton twitchUserId={row.original.twitch_user_id} t={t} />
				</div>
			),
		},
	];
}
