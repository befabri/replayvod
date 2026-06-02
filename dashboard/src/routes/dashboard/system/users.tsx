import { createFileRoute } from "@tanstack/react-router";
import { useSelector } from "@tanstack/react-store";
import { useMemo } from "react";
import { useTranslation } from "react-i18next";
import { TitledLayout } from "@/components/layout/titled-layout";
import { DataTable } from "@/components/ui/data-table";
import { useUsers } from "@/features/users";
import { userColumns } from "@/features/users/components/columns";
import { authStore } from "@/stores/auth";

export const Route = createFileRoute("/dashboard/system/users")({
	component: UsersPage,
});

function UsersPage() {
	const { t } = useTranslation();
	const currentUser = useSelector(authStore, (s) => s.user);
	const { data: users, isLoading, error } = useUsers();

	const columns = useMemo(
		() => userColumns(currentUser?.id, t),
		[currentUser?.id, t],
	);

	return (
		<TitledLayout title={t("users.title")}>
			{isLoading && (
				<div className="text-muted-foreground">{t("common.loading")}</div>
			)}

			{error && (
				<div className="rounded-md bg-destructive/10 border border-destructive/20 p-4 text-destructive text-sm">
					{t("users.failed_to_load")}: {error.message}
				</div>
			)}

			{users && (
				<DataTable
					columns={columns}
					data={users}
					emptyMessage={t("users.empty")}
				/>
			)}
		</TitledLayout>
	);
}
