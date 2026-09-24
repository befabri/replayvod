import { createFileRoute } from "@tanstack/react-router";
import { useTranslation } from "react-i18next";
import { TitledLayout } from "@/components/layout/titled-layout";
import { Alert } from "@/components/ui/alert";
import {
	RecordingWebhookCard,
	RecordingWebhookCardSkeleton,
	RecordingWebhookDeliveries,
	RecordingWebhookDeliveriesSkeleton,
	useRecordingWebhookConfig,
} from "@/features/recording-webhook";
import { requireRole } from "@/lib/route-guards";

export const Route = createFileRoute("/dashboard/system/webhook")({
	beforeLoad: requireRole("owner"),
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
				<div className="grid gap-6">
					<RecordingWebhookCardSkeleton />
					<RecordingWebhookDeliveriesSkeleton />
				</div>
			)}
			{config.isError && (
				<Alert variant="destructive">
					{config.error?.message ?? t("webhook.load_failed")}
				</Alert>
			)}
			{config.data && (
				<div className="grid gap-6">
					<RecordingWebhookCard data={config.data} />
					<RecordingWebhookDeliveries />
				</div>
			)}
		</TitledLayout>
	);
}
