import type { ColumnDef } from "@tanstack/react-table";
import type { TFunction } from "i18next";
import type { UserInfo } from "@/features/users";
import { useUpdateUserRole } from "@/features/users";
import { isRole, ROLES, type Role } from "@/stores/auth";

type UserRoleLabelKey =
	| "users.role_viewer"
	| "users.role_admin"
	| "users.role_owner";
const ROLE_LABEL_KEYS: Record<Role, UserRoleLabelKey> = {
	viewer: "users.role_viewer",
	admin: "users.role_admin",
	owner: "users.role_owner",
};

// RoleSelect is a thin cell component so the mutation hook mounts per
// row — keeps each row's pending/error state isolated.
function RoleSelect({
	user,
	isSelf,
	t,
}: {
	user: UserInfo;
	isSelf: boolean;
	t: TFunction;
}) {
	const update = useUpdateUserRole();
	const value = isRole(user.role) ? user.role : "";
	return (
		<select
			value={value}
			disabled={isSelf}
			onChange={(e) => {
				const role = e.target.value;
				if (!isRole(role)) return;
				update.mutate({
					user_id: user.id,
					role,
				});
			}}
			className="rounded-md border border-border bg-background px-2 py-1 text-sm disabled:opacity-60"
		>
			{value === "" && (
				<option value="" disabled>
					{t("users.role_unknown")}
				</option>
			)}
			{ROLES.map((role) => (
				<option key={role} value={role}>
					{t(ROLE_LABEL_KEYS[role])}
				</option>
			))}
		</select>
	);
}

export function userColumns(
	currentUserId: string | undefined,
	t: TFunction,
): ColumnDef<UserInfo>[] {
	return [
		{
			accessorKey: "display_name",
			header: t("users.col_user"),
			enableSorting: true,
			cell: ({ row }) => {
				const u = row.original;
				const isSelf = u.id === currentUserId;
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
							<span className="text-xs text-muted-foreground">
								{t("users.you")}
							</span>
						)}
					</div>
				);
			},
		},
		{
			accessorKey: "login",
			header: t("users.col_login"),
			enableSorting: true,
			cell: ({ row }) => (
				<span className="text-muted-foreground">@{row.original.login}</span>
			),
		},
		{
			accessorKey: "role",
			header: t("users.col_role"),
			enableSorting: true,
			cell: ({ row }) => (
				<RoleSelect
					user={row.original}
					isSelf={row.original.id === currentUserId}
					t={t}
				/>
			),
		},
		{
			accessorKey: "created_at",
			header: t("users.col_joined"),
			enableSorting: true,
			cell: ({ row }) => (
				<span className="text-muted-foreground">
					{new Date(row.original.created_at).toLocaleDateString()}
				</span>
			),
		},
	];
}
