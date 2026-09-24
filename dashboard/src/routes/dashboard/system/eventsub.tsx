import { createFileRoute } from "@tanstack/react-router";
import { useTranslation } from "react-i18next";
import { TitledLayout } from "@/components/layout/titled-layout";
import { Alert } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import {
	EventSubSetupCard,
	EventSubSetupCardSkeleton,
	useEventSubConfig,
	useSnapshotNow,
	useSnapshots,
	useSubscriptions,
} from "@/features/eventsub";
import { QuotaCard } from "@/features/eventsub/components/QuotaCard";
import {
	SnapshotChart,
	SnapshotChartSkeleton,
} from "@/features/eventsub/components/SnapshotChart";
import { SubscriptionsTable } from "@/features/eventsub/components/SubscriptionsTable";
import { requireRole } from "@/lib/route-guards";

export const Route = createFileRoute("/dashboard/system/eventsub")({
	beforeLoad: requireRole("owner"),
	component: EventSubPage,
});

function EventSubPage() {
	const { t } = useTranslation();
	const subs = useSubscriptions();
	const snapshots = useSnapshots();
	const poll = useSnapshotNow();
	const config = useEventSubConfig();

	return (
		<TitledLayout
			title={t("eventsub.title")}
			actions={
				<Button onClick={() => poll.mutate()} disabled={poll.isPending}>
					{poll.isPending ? t("eventsub.polling") : t("eventsub.poll_now")}
				</Button>
			}
		>
			<p className="text-muted-foreground mb-6 -mt-6">
				{t("eventsub.description")}
			</p>

			{config.isLoading && (
				<div className="mb-6">
					<EventSubSetupCardSkeleton />
				</div>
			)}
			{config.isError && (
				<Alert variant="destructive" className="mb-4">
					{config.error?.message ?? t("eventsub.config_load_failed")}
				</Alert>
			)}
			{config.data && (
				<div className="mb-6">
					<EventSubSetupCard data={config.data} />
				</div>
			)}

			{poll.isError && (
				<Alert variant="destructive" className="mb-4">
					{poll.error?.message ?? t("eventsub.poll_failed")}
				</Alert>
			)}

			<QuotaCard />

			<section className="mb-8">
				<h2 className="text-xl font-medium mb-3">{t("eventsub.snapshots")}</h2>
				{snapshots.isLoading && <SnapshotChartSkeleton />}
				{snapshots.data && snapshots.data.data.length === 0 && (
					<div className="text-muted-foreground text-sm">
						{t("eventsub.no_snapshots")}
					</div>
				)}
				{snapshots.data && snapshots.data.data.length > 0 && (
					<SnapshotChart data={snapshots.data.data} />
				)}
			</section>

			<section>
				<h2 className="text-xl font-medium mb-3">
					{t("eventsub.subscriptions")}
					{subs.data ? ` (${subs.data.total})` : null}
				</h2>
				{(subs.isLoading || subs.data) && (
					<SubscriptionsTable
						rows={subs.data?.data ?? []}
						loading={subs.isLoading}
					/>
				)}
			</section>
		</TitledLayout>
	);
}
