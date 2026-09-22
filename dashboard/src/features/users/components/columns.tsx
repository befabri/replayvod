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

function RoleSelect({
	user,
	isSelf,
	callerIsOwner,
	t,
}: {
	user: UserInfo;
	isSelf: boolean;
	callerIsOwner: boolean;
	t: TFunction;
}) {
	const update = useUpdateUserRole();
	const value = isRole(user.role) ? user.role : "";
	const roles = callerIsOwner ? ROLES : ROLES.filter((r) => r !== "owner");
	return (
		<select
			value={value}
			disabled={isSelf || (!callerIsOwner && value === "owner")}
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
			{!callerIsOwner && value === "owner" && (
				<option value="owner" disabled>
					{t(ROLE_LABEL_KEYS.owner)}
				</option>
			)}
			{roles.map((role) => (
				<option key={role} value={role}>
					{t(ROLE_LABEL_KEYS[role])}
				</option>
			))}
		</select>
	);
}

export function userColumns(
	currentUserId: string | undefined,
	callerIsOwner: boolean,
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
			accessorKey: "id",
			header: t("users.col_id"),
			enableSorting: true,
			cell: ({ row }) => (
				<span className="font-mono text-muted-foreground">
					{row.original.id}
				</span>
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
					callerIsOwner={callerIsOwner}
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
