import { ArrowsClockwiseIcon } from "@phosphor-icons/react";
import type { ReactNode } from "react";
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
import { LoadingState } from "@/components/ui/loading-state";
import { BadgeSkeleton, Skeleton } from "@/components/ui/skeleton";
import { TimestampValue } from "@/components/ui/timestamp";
import {
	useRecordingWebhookDeliveries,
	useRetryRecordingWebhookDelivery,
} from "../queries";

const ROW_CLASS =
	"flex flex-wrap items-center justify-between gap-2 rounded-md border border-border p-2 text-sm";

const SKELETON_ROWS = ["delivery-1", "delivery-2", "delivery-3"];

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

export function RecordingWebhookDeliveries() {
	const { t } = useTranslation();
	const deliveries = useRecordingWebhookDeliveries();
	const rows = deliveries.data ?? [];

	return (
		<DeliveriesCard>
			{deliveries.isLoading && (
				<LoadingState>
					<DeliveryRowsSkeleton />
				</LoadingState>
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
		</DeliveriesCard>
	);
}

export function RecordingWebhookDeliveriesSkeleton() {
	return (
		<LoadingState>
			<DeliveriesCard>
				<DeliveryRowsSkeleton />
			</DeliveriesCard>
		</LoadingState>
	);
}

function DeliveriesCard({ children }: { children: ReactNode }) {
	const { t } = useTranslation();
	return (
		<Card>
			<CardHeader>
				<CardTitle>{t("webhook.deliveries_title")}</CardTitle>
				<CardDescription>{t("webhook.deliveries_description")}</CardDescription>
			</CardHeader>
			<CardContent>{children}</CardContent>
		</Card>
	);
}

function DeliveryRowsSkeleton() {
	return (
		<ul className="grid gap-2">
			{SKELETON_ROWS.map((key) => (
				<li key={key} className={ROW_CLASS}>
					<div className="flex items-center gap-2">
						<BadgeSkeleton />
						<div className="flex h-4 items-center">
							<Skeleton className="h-3 w-32" />
						</div>
					</div>
					<div className="flex h-4 items-center">
						<Skeleton className="h-3 w-40" />
					</div>
				</li>
			))}
		</ul>
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
		<li className={ROW_CLASS}>
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
