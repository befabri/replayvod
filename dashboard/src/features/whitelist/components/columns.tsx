import type { ColumnDef } from "@tanstack/react-table";
import type { TFunction } from "i18next";
import { Button } from "@/components/ui/button";
import { Skeleton } from "@/components/ui/skeleton";
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
		<Button
			disabled={remove.isPending}
			onClick={() => remove.mutate({ twitch_user_id: twitchUserId })}
			variant="link-destructive"
			size="inline"
		>
			{t("whitelist.remove")}
		</Button>
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
			meta: {
				skeleton: (
					<div className="flex justify-end">
						<Skeleton className="h-5.5 w-16" />
					</div>
				),
			},
			cell: ({ row }) => (
				<div className="text-right">
					<RemoveButton twitchUserId={row.original.twitch_user_id} t={t} />
				</div>
			),
		},
	];
}
