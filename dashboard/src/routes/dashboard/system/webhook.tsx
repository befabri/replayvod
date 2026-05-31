import { createFileRoute } from "@tanstack/react-router";
import { useTranslation } from "react-i18next";
import { TitledLayout } from "@/components/layout/titled-layout";
import {
	RecordingWebhookCard,
	RecordingWebhookDeliveries,
	useRecordingWebhookConfig,
} from "@/features/recording-webhook";

export const Route = createFileRoute("/dashboard/system/webhook")({
	component: WebhookPage,
});

function WebhookPage() {
	const { t } = useTranslation();
	const config = useRecordingWebhookConfig();

	return (
		<TitledLayout title={t("webhook.title")}>
			<p className="text-muted-foreground mb-6 -mt-6">
				{t("webhook.page_description")}
			</p>

			{config.isLoading && (
				<div className="text-muted-foreground">{t("common.loading")}</div>
			)}
			{config.isError && (
				<div className="rounded-md bg-destructive/10 border border-destructive/20 p-3 text-destructive text-sm">
					{config.error?.message ?? t("webhook.load_failed")}
				</div>
			)}
			{config.data && (
				<div className="grid max-w-2xl gap-6">
					<RecordingWebhookCard data={config.data} />
					<RecordingWebhookDeliveries />
				</div>
			)}
		</TitledLayout>
	);
}
