import { createFileRoute } from "@tanstack/react-router";
import { useTranslation } from "react-i18next";
import { TitledLayout } from "@/components/layout/titled-layout";
import { Alert } from "@/components/ui/alert";
import { useSettings } from "@/features/settings";
import {
	PlaybackSettingsForm,
	PlaybackSettingsFormSkeleton,
} from "@/features/settings/components/PlaybackSettingsForm";
import {
	SettingsForm,
	SettingsFormSkeleton,
} from "@/features/settings/components/SettingsForm";

export const Route = createFileRoute("/dashboard/settings")({
	component: SettingsPage,
});

function SettingsPage() {
	const { t } = useTranslation();
	const { data, isLoading, error } = useSettings();

	return (
		<TitledLayout title={t("settings.title")}>
			<p className="text-muted-foreground mb-6 -mt-6">
				{t("settings.description")}
			</p>

			{isLoading && (
				<div className="space-y-6">
					<SettingsFormSkeleton />
					<PlaybackSettingsFormSkeleton />
				</div>
			)}
			{error && (
				<Alert variant="destructive">
					{t("settings.failed_to_load")}: {error.message}
				</Alert>
			)}

			{data && (
				<div className="space-y-6">
					<SettingsForm key={data.user_id} data={data} />
					<PlaybackSettingsForm key={data.user_id} data={data} />
				</div>
			)}
		</TitledLayout>
	);
}
