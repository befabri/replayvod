import { ArrowsClockwiseIcon } from "@phosphor-icons/react";
import { useTranslation } from "react-i18next";
import { toast } from "sonner";
import type { RecordingWebhookDeliveryResponse as Delivery } from "@/api/generated/trpc";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import {
	Card,
	CardContent,
	CardDescription,
	CardHeader,
	CardTitle,
} from "@/components/ui/card";
import { TimestampValue } from "@/components/ui/timestamp";
import {
	useRecordingWebhookDeliveries,
	useRetryRecordingWebhookDelivery,
} from "../queries";

// outcomeVariant maps a delivery outcome to a badge color.
function outcomeVariant(outcome: string): "green" | "yellow" | "red" | "muted" {
	switch (outcome) {
		case "delivered":
			return "green";
		case "rejected":
			return "yellow";
		case "failed":
			return "red";
		case "pending":
		case "delivering":
			return "muted";
		default:
			return "muted";
	}
}

// RecordingWebhookDeliveries shows the durable delivery log so an owner can
// tell at a glance whether deliveries are landing and retry failed attempts.
export function RecordingWebhookDeliveries() {
	const { t } = useTranslation();
	const deliveries = useRecordingWebhookDeliveries();
	const rows = deliveries.data ?? [];

	return (
		<Card>
			<CardHeader>
				<CardTitle>{t("webhook.deliveries_title")}</CardTitle>
				<CardDescription>{t("webhook.deliveries_description")}</CardDescription>
			</CardHeader>
			<CardContent>
				{deliveries.isLoading && (
					<div className="text-muted-foreground text-sm">
						{t("common.loading")}
					</div>
				)}
				{!deliveries.isLoading && rows.length === 0 && (
					<div className="text-muted-foreground text-sm">
						{t("webhook.deliveries_empty")}
					</div>
				)}
				{rows.length > 0 && (
					<ul className="grid gap-2">
						{rows.map((d) => (
							<DeliveryRow key={d.id} delivery={d} />
						))}
					</ul>
				)}
			</CardContent>
		</Card>
	);
}

function DeliveryRow({ delivery }: { delivery: Delivery }) {
	const { t, i18n } = useTranslation();
	const retry = useRetryRecordingWebhookDelivery();
	const retrying = retry.isPending && retry.variables?.id === delivery.id;
	const canRetry =
		delivery.outcome === "failed" || delivery.outcome === "rejected";
	const onRetry = () => {
		retry.mutate(
			{ id: delivery.id },
			{
				onSuccess: () => toast.success(t("webhook.retry_queued")),
				onError: (err) => toast.error(err.message || t("webhook.retry_failed")),
			},
		);
	};
	return (
		<li className="flex flex-wrap items-center justify-between gap-2 rounded-md border border-border p-2 text-sm">
			<div className="flex items-center gap-2">
				<Badge variant={outcomeVariant(delivery.outcome)}>
					{t(`webhook.outcome_${delivery.outcome}`)}
				</Badge>
				<span className="font-mono text-xs">{delivery.event}</span>
				{delivery.test && (
					<Badge variant="muted">{t("webhook.delivery_test")}</Badge>
				)}
			</div>
			<div className="flex items-center gap-3 text-muted-foreground text-xs">
				{delivery.attempts > 0 && (
					<span>
						{t("webhook.delivery_attempts", {
							count: delivery.attempts,
						})}
					</span>
				)}
				{delivery.status > 0 && <span>HTTP {delivery.status}</span>}
				{delivery.error && (
					<span className="text-destructive" title={delivery.error}>
						{delivery.error}
					</span>
				)}
				<TimestampValue iso={delivery.time} locale={i18n.language} />
				{canRetry && (
					<Button
						type="button"
						variant="ghost"
						size="icon-xs"
						aria-label={t("webhook.retry_delivery")}
						title={t("webhook.retry_delivery")}
						disabled={retrying}
						onClick={onRetry}
					>
						<ArrowsClockwiseIcon />
					</Button>
				)}
			</div>
		</li>
	);
}
