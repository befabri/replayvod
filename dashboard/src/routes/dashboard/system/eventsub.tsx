import { createFileRoute } from "@tanstack/react-router"
import { useTranslation } from "react-i18next"
import {
	useLatestSnapshot,
	useSnapshotNow,
	useSnapshots,
	useSubscriptions,
	useUnsubscribe,
} from "@/features/eventsub"

// Row sub shape as the query actually returns it; condition is serialized
// as json.RawMessage server-side so TRPC infers it as potentially absent.
type SubRowData = {
	id: string
	type: string
	version: string
	status: string
	cost: number
	broadcaster_id?: string
}

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

function QuotaCard() {
	const { t } = useTranslation()
	const { data, isLoading } = useLatestSnapshot()
	const snap = data?.snapshot

	if (isLoading) {
		return (
			<div className="rounded-lg border border-border bg-card p-4 mb-6">
				<div className="text-muted-foreground text-sm">
					{t("common.loading")}
				</div>
			</div>
		)
	}
	if (!snap) {
		return (
			<div className="rounded-lg border border-border bg-card p-4 mb-6 text-sm text-muted-foreground">
				{t("eventsub.no_snapshot_yet")}
			</div>
		)
	}

	const quotaPct =
		snap.max_total_cost > 0
			? Math.min(100, Math.round((snap.total_cost / snap.max_total_cost) * 100))
			: 0

	return (
		<div className="rounded-lg border border-border bg-card p-4 mb-6">
			<div className="flex items-center justify-between mb-3">
				<div>
					<div className="text-sm text-muted-foreground">
						{t("eventsub.quota_label")}
					</div>
					<div className="text-2xl font-medium">
						{snap.total_cost} / {snap.max_total_cost}
					</div>
				</div>
				<div className="text-right">
					<div className="text-sm text-muted-foreground">
						{t("eventsub.active_subs")}
					</div>
					<div className="text-2xl font-medium">{snap.total}</div>
				</div>
			</div>
			<div className="h-2 w-full rounded-full bg-muted overflow-hidden">
				<div
					className="h-full bg-primary transition-all"
					style={{ width: `${quotaPct}%` }}
				/>
			</div>
			<div className="mt-2 text-xs text-muted-foreground">
				{t("eventsub.snapshot_at")}:{" "}
				{new Date(snap.fetched_at).toLocaleString()}
			</div>
		</div>
	)
}

function SnapshotChart({
	data,
}: {
	data: { id: number; fetched_at: string; total_cost: number; total: number }[]
}) {
	// Newest first from the server; reverse for left-to-right chronological.
	const points = [...data].reverse()
	const maxCost = Math.max(1, ...points.map((p) => p.total_cost))

	return (
		<div className="rounded-lg border border-border bg-card p-4">
			<div className="flex items-end gap-1 h-24">
				{points.map((p) => {
					const h = (p.total_cost / maxCost) * 100
					return (
						<div
							key={p.id}
							title={`${new Date(p.fetched_at).toLocaleString()} — cost ${p.total_cost}`}
							className="flex-1 min-w-0 bg-primary/70 rounded-sm"
							style={{ height: `${h}%` }}
						/>
					)
				})}
			</div>
			<div className="mt-2 text-xs text-muted-foreground">
				{points.length > 0 && (
					<>
						{new Date(points[0].fetched_at).toLocaleDateString()} →{" "}
						{new Date(points[points.length - 1].fetched_at).toLocaleDateString()}
					</>
				)}
			</div>
		</div>
	)
}

function SubRow({ sub }: { sub: SubRowData }) {
	const { t } = useTranslation()
	const unsub = useUnsubscribe()
	return (
		<tr className="border-t border-border">
			<td className="px-3 py-2 font-mono text-xs">
				{sub.type} <span className="text-muted-foreground">v{sub.version}</span>
			</td>
			<td className="px-3 py-2 font-mono text-xs">
				{sub.broadcaster_id ?? "—"}
			</td>
			<td className="px-3 py-2">{sub.status}</td>
			<td className="px-3 py-2 text-right">{sub.cost}</td>
			<td className="px-3 py-2 text-right">
				<button
					type="button"
					onClick={() => unsub.mutate({ id: sub.id, reason: "manual" })}
					disabled={unsub.isPending}
					className="text-destructive hover:underline text-xs disabled:opacity-60"
				>
					{t("eventsub.unsubscribe")}
				</button>
			</td>
		</tr>
	)
}
