import { WarningCircle } from "@phosphor-icons/react";
import { createFileRoute, Link } from "@tanstack/react-router";
import { useTranslation } from "react-i18next";
import { buttonVariants } from "@/components/ui/button";
import { EmptyState } from "@/components/ui/empty-state";

export const Route = createFileRoute("/$")({
	component: NotFoundPage,
});

function NotFoundPage() {
	const { t } = useTranslation();
	return (
		<div className="min-h-screen bg-background text-foreground flex items-center justify-center p-4">
			<EmptyState
				icon={<WarningCircle weight="duotone" />}
				title={t("errors.not_found_title")}
				description={t("errors.not_found_description")}
				action={
					<Link to="/dashboard" className={buttonVariants()}>
						{t("nav.dashboard")}
					</Link>
				}
				className="w-full max-w-md"
			/>
		</div>
	);
}
