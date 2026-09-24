import type { ColumnDef } from "@tanstack/react-table";
import type { TFunction } from "i18next";
import { Avatar } from "@/components/ui/avatar";
import {
	Select,
	SelectContent,
	SelectItem,
	SelectTrigger,
	SelectValue,
} from "@/components/ui/select";
import { FieldSkeleton, Skeleton } from "@/components/ui/skeleton";
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
	const value = isRole(user.role) ? user.role : null;
	const lockedOwner = !callerIsOwner && value === "owner";
	const items = [
		...(lockedOwner
			? [{ value: "owner", label: t(ROLE_LABEL_KEYS.owner), disabled: true }]
			: []),
		...(callerIsOwner ? ROLES : ROLES.filter((r) => r !== "owner")).map(
			(role) => ({
				value: role,
				label: t(ROLE_LABEL_KEYS[role]),
				disabled: false,
			}),
		),
	];
	return (
		<Select
			value={value}
			items={items}
			disabled={isSelf || lockedOwner}
			onValueChange={(next) => {
				if (typeof next !== "string" || !isRole(next)) return;
				update.mutate({ user_id: user.id, role: next });
			}}
		>
			<SelectTrigger className="w-36" aria-label={t("users.col_role")}>
				<SelectValue placeholder={t("users.role_unknown")} />
			</SelectTrigger>
			<SelectContent>
				{items.map((item) => (
					<SelectItem
						key={item.value}
						value={item.value}
						disabled={item.disabled}
					>
						{item.label}
					</SelectItem>
				))}
			</SelectContent>
		</Select>
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
			meta: {
				skeleton: (
					<div className="flex items-center gap-2">
						<Skeleton className="size-8 shrink-0 rounded-full" />
						<Skeleton className="h-3.5 w-24" />
					</div>
				),
			},
			cell: ({ row }) => {
				const u = row.original;
				const isSelf = u.id === currentUserId;
				return (
					<div className="flex items-center gap-2">
						<Avatar src={u.profile_image_url} name={u.display_name} size="md" />
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
			meta: { skeleton: <FieldSkeleton className="w-36" /> },
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
