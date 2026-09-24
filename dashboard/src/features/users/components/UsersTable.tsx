import { useSelector } from "@tanstack/react-store";
import { useMemo } from "react";
import { useTranslation } from "react-i18next";
import { QueryTable } from "@/components/ui/query-table";
import { useUsers } from "@/features/users";
import { userColumns } from "@/features/users/components/columns";
import { authStore } from "@/stores/auth";

export function UsersTable() {
	const { t } = useTranslation();
	const currentUser = useSelector(authStore, (s) => s.user);
	const users = useUsers();

	const columns = useMemo(
		() => userColumns(currentUser?.id, currentUser?.role === "owner", t),
		[currentUser?.id, currentUser?.role, t],
	);

	return (
		<QueryTable
			query={users}
			columns={columns}
			getRows={(data) => data}
			emptyMessage={t("users.empty")}
			errorLabel={t("users.failed_to_load")}
		/>
	);
}
