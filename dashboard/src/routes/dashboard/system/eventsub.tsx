import { createFileRoute } from "@tanstack/react-router"
import { useTranslation } from "react-i18next"
import { QuotaCard } from "@/features/eventsub/components/QuotaCard"
import { SnapshotChart } from "@/features/eventsub/components/SnapshotChart"
import { SubRow } from "@/features/eventsub/components/SubRow"
import {
	useSnapshotNow,
	useSnapshots,
	useSubscriptions,
} from "@/features/eventsub"

export const Route = createFileRoute("/dashboard/system/eventsub")({
	component: EventSubPage,
})

function EventSubPage() {
	const { t } = useTranslation()
	const subs = useSubscriptions()
	const snapshots = useSnapshots()
	const poll = useSnapshotNow()

	return (
		<div className="p-8 max-w-5xl">
			<div className="flex items-start justify-between gap-4 mb-6">
				<div>
					<h1 className="text-3xl font-heading font-bold mb-2">
						{t("eventsub.title")}
					</h1>
					<p className="text-sm text-muted-foreground">
						{t("eventsub.description")}
					</p>
				</div>
				<button
					type="button"
					onClick={() => poll.mutate()}
					disabled={poll.isPending}
					className="rounded-md bg-primary px-4 py-2 text-sm font-medium text-primary-foreground disabled:opacity-60"
				>
					{poll.isPending ? t("eventsub.polling") : t("eventsub.poll_now")}
				</button>
			</div>

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
				{subs.data && subs.data.data.length === 0 && (
					<div className="text-muted-foreground text-sm">
						{t("eventsub.no_subscriptions")}
					</div>
				)}
				{subs.data && subs.data.data.length > 0 && (
					<div className="rounded-lg border border-border overflow-hidden">
						<table className="w-full text-sm">
							<thead className="bg-muted/50">
								<tr>
									<th className="text-left px-3 py-2 font-medium">
										{t("eventsub.col_type")}
									</th>
									<th className="text-left px-3 py-2 font-medium">
										{t("eventsub.col_broadcaster")}
									</th>
									<th className="text-left px-3 py-2 font-medium">
										{t("eventsub.col_status")}
									</th>
									<th className="text-right px-3 py-2 font-medium">
										{t("eventsub.col_cost")}
									</th>
									<th className="text-right px-3 py-2 font-medium" />
								</tr>
							</thead>
							<tbody>
								{subs.data.data.map((s) => (
									<SubRow key={s.id} sub={s} />
								))}
							</tbody>
						</table>
					</div>
				)}
			</section>
		</div>
	)
}
