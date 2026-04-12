import { useTranslation } from "react-i18next"
import { useLatestSnapshot } from "@/features/eventsub"

export function QuotaCard() {
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
