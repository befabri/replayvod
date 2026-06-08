import { createFileRoute } from "@tanstack/react-router";
import { useSelector } from "@tanstack/react-store";
import { useMemo } from "react";
import { useTranslation } from "react-i18next";
import { TitledLayout } from "@/components/layout/titled-layout";
import { QueryTable } from "@/components/ui/query-table";
import { useUsers } from "@/features/users";
import { userColumns } from "@/features/users/components/columns";
import { authStore } from "@/stores/auth";

export const Route = createFileRoute("/dashboard/system/users")({
	component: UsersPage,
});

function UsersPage() {
	const { t } = useTranslation();
	const currentUser = useSelector(authStore, (s) => s.user);
	const users = useUsers();

	const columns = useMemo(
		() => userColumns(currentUser?.id, t),
		[currentUser?.id, t],
	);

	return (
		<TitledLayout title={t("users.title")}>
			<QueryTable
				query={users}
				columns={columns}
				getRows={(data) => data}
				emptyMessage={t("users.empty")}
				errorLabel={t("users.failed_to_load")}
			/>
		</TitledLayout>
	);
}
