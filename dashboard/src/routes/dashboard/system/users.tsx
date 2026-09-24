import { createFileRoute } from "@tanstack/react-router";
import { useTranslation } from "react-i18next";
import { DocsLink } from "@/components/layout/docs-link";
import { TitledLayout } from "@/components/layout/titled-layout";
import { InvitesSection } from "@/features/invites/components/InvitesSection";
import { UsersTable } from "@/features/users/components/UsersTable";

export const Route = createFileRoute("/dashboard/system/users")({
	component: UsersPage,
});

function UsersPage() {
	const { t } = useTranslation();

	return (
		<TitledLayout
			title={t("users.title")}
			actions={<DocsLink page="access/">{t("docs.access")}</DocsLink>}
		>
			<UsersTable />
			<InvitesSection />
		</TitledLayout>
	);
}
