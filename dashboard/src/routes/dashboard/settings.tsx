import { createFileRoute } from "@tanstack/react-router"
import { useTranslation } from "react-i18next"
import { SettingsForm } from "@/features/settings/components/SettingsForm"
import { useSettings } from "@/features/settings"

export const Route = createFileRoute("/dashboard/settings")({
	component: SettingsPage,
})

function SettingsPage() {
	const { t } = useTranslation()
	const { data, isLoading, error } = useSettings()

	return (
		<div className="p-8 max-w-2xl">
			<h1 className="text-3xl font-heading font-bold mb-2">
				{t("settings.title")}
			</h1>
			<p className="text-sm text-muted-foreground mb-6">
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
	)
}
