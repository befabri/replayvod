import { WarningCircle } from "@phosphor-icons/react";
import { createFileRoute, Link } from "@tanstack/react-router";
import { useTranslation } from "react-i18next";
import { TitledLayout } from "@/components/layout/titled-layout";
import { buttonVariants } from "@/components/ui/button";
import { EmptyState } from "@/components/ui/empty-state";

export const Route = createFileRoute("/dashboard/$")({
	component: DashboardNotFound,
});

function DashboardNotFound() {
	const { t } = useTranslation();
	return (
		<TitledLayout title="404">
			<EmptyState
				icon={<WarningCircle weight="duotone" />}
				title={t("errors.not_found_title")}
				description={t("errors.not_found_description")}
				action={
					<Link to="/dashboard" className={buttonVariants()}>
						{t("nav.dashboard")}
					</Link>
				}
			/>
		</TitledLayout>
	);
}
