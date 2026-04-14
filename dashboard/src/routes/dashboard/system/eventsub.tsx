import { createFileRoute } from "@tanstack/react-router";
import { useMemo } from "react";
import { useTranslation } from "react-i18next";
import { TitledLayout } from "@/components/layout/titled-layout";
import { DataTable } from "@/components/ui/data-table";
import {
	useSnapshotNow,
	useSnapshots,
	useSubscriptions,
} from "@/features/eventsub";
import { subscriptionColumns } from "@/features/eventsub/components/columns";
import { QuotaCard } from "@/features/eventsub/components/QuotaCard";
import { SnapshotChart } from "@/features/eventsub/components/SnapshotChart";

export const Route = createFileRoute("/dashboard/system/eventsub")({
	component: EventSubPage,
});

function EventSubPage() {
	const { t } = useTranslation();
	const subs = useSubscriptions();
	const snapshots = useSnapshots();
	const poll = useSnapshotNow();

	const columns = useMemo(() => subscriptionColumns(t), [t]);

	return (
		<TitledLayout
			title={t("eventsub.title")}
			actions={
				<button
					type="button"
					onClick={() => poll.mutate()}
					disabled={poll.isPending}
					className="rounded-md bg-primary px-4 py-2 text-sm font-medium text-primary-foreground disabled:opacity-60"
				>
					{poll.isPending ? t("eventsub.polling") : t("eventsub.poll_now")}
				</button>
			}
		>
			<p className="text-muted-foreground mb-6 -mt-6">
				{t("eventsub.description")}
			</p>

			{poll.isError && (
				<div className="mb-4 rounded-md bg-destructive/10 border border-destructive/20 p-3 text-destructive text-sm">
					{poll.error?.message ?? t("eventsub.poll_failed")}
				</div>
			)}

			<QuotaCard />

			<section className="mb-8">
				<h2 className="text-xl font-medium mb-3">{t("eventsub.snapshots")}</h2>
				{snapshots.isLoading && (
					<div className="text-muted-foreground">{t("common.loading")}</div>
				)}
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
					{t("eventsub.subscriptions")} ({subs.data?.total ?? 0})
				</h2>
				{subs.isLoading && (
					<div className="text-muted-foreground">{t("common.loading")}</div>
				)}
				{subs.data && (
					<DataTable
						columns={columns}
						data={subs.data.data}
						emptyMessage={t("eventsub.no_subscriptions")}
					/>
				)}
			</section>
		</TitledLayout>
	);
}
