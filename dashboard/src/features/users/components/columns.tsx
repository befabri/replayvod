import type { ColumnDef } from "@tanstack/react-table"
import type { UserInfo } from "@/features/users"
import { useUpdateUserRole } from "@/features/users"
import type { Role } from "@/stores/auth"

// RoleSelect is a thin cell component so the mutation hook mounts per
// row — keeps each row's pending/error state isolated.
function RoleSelect({ user, isSelf }: { user: UserInfo; isSelf: boolean }) {
	const update = useUpdateUserRole()
	return (
		<select
			value={user.role}
			disabled={isSelf}
			onChange={(e) =>
				update.mutate({
					user_id: user.id,
					role: e.target.value as Role,
				})
			}
			className="rounded-md border border-border bg-background px-2 py-1 text-sm disabled:opacity-60"
		>
			<option value="viewer">viewer</option>
			<option value="admin">admin</option>
			<option value="owner">owner</option>
		</select>
	)
}

export function userColumns(currentUserId: string | undefined): ColumnDef<UserInfo>[] {
	return [
		{
			accessorKey: "display_name",
			header: "User",
			enableSorting: true,
			cell: ({ row }) => {
				const u = row.original
				const isSelf = u.id === currentUserId
				return (
					<div className="flex items-center gap-2">
						{u.profile_image_url && (
							<img
								src={u.profile_image_url}
								alt=""
								className="w-8 h-8 rounded-full"
							/>
						)}
						<span>{u.display_name}</span>
						{isSelf && (
							<span className="text-xs text-muted-foreground">(you)</span>
						)}
					</div>
				)
			},
		},
		{
			accessorKey: "login",
			header: "Login",
			enableSorting: true,
			cell: ({ row }) => (
				<span className="text-muted-foreground">@{row.original.login}</span>
			),
		},
		{
			accessorKey: "role",
			header: "Role",
			enableSorting: true,
			cell: ({ row }) => (
				<RoleSelect
					user={row.original}
					isSelf={row.original.id === currentUserId}
				/>
			),
		},
		{
			accessorKey: "created_at",
			header: "Joined",
			enableSorting: true,
			cell: ({ row }) => (
				<span className="text-muted-foreground">
					{new Date(row.original.created_at).toLocaleDateString()}
				</span>
			),
		},
	]
}
