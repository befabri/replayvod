import type { ColumnDef } from "@tanstack/react-table";
import type { WhitelistEntryInfo } from "@/features/whitelist";
import { useRemoveWhitelist } from "@/features/whitelist";

function RemoveButton({ twitchUserId }: { twitchUserId: string }) {
	const remove = useRemoveWhitelist();
	return (
		<button
			type="button"
			disabled={remove.isPending}
			onClick={() => remove.mutate({ twitch_user_id: twitchUserId })}
			className="text-destructive hover:underline disabled:opacity-60"
		>
			Remove
		</button>
	);
}

export const whitelistColumns: ColumnDef<WhitelistEntryInfo>[] = [
	{
		accessorKey: "twitch_user_id",
		header: "Twitch User ID",
		enableSorting: true,
		cell: ({ row }) => (
			<span className="font-mono">{row.original.twitch_user_id}</span>
		),
	},
	{
		accessorKey: "added_at",
		header: "Added",
		enableSorting: true,
		cell: ({ row }) => (
			<span className="text-muted-foreground">
				{new Date(row.original.added_at).toLocaleString()}
			</span>
		),
	},
	{
		id: "actions",
		header: () => <span className="text-right w-full block">Actions</span>,
		enableSorting: false,
		cell: ({ row }) => (
			<div className="text-right">
				<RemoveButton twitchUserId={row.original.twitch_user_id} />
			</div>
		),
	},
];
