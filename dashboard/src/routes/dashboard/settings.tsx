import { createFileRoute } from "@tanstack/react-router";
import { useTranslation } from "react-i18next";
import { TitledLayout } from "@/components/layout/titled-layout";
import { useSettings } from "@/features/settings";
import { SettingsForm } from "@/features/settings/components/SettingsForm";

export const Route = createFileRoute("/dashboard/settings")({
	component: SettingsPage,
});

function SettingsPage() {
	const { t } = useTranslation();
	const { data, isLoading, error } = useSettings();

	return (
		<TitledLayout title={t("settings.title")}>
			<div className="max-w-2xl">
				<p className="text-muted-foreground mb-6 -mt-6">
					{t("settings.description")}
				</p>

				{isLoading && (
					<div className="text-muted-foreground">{t("common.loading")}</div>
				)}
				{error && (
					<div className="rounded-md bg-destructive/10 border border-destructive/20 p-4 text-destructive text-sm">
						{t("settings.failed_to_load")}: {error.message}
					</div>
				)}

				{data && <SettingsForm data={data} />}
			</div>
		</TitledLayout>
	);
}
