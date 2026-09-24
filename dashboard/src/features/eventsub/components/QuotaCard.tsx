import type { ReactNode } from "react";
import { useTranslation } from "react-i18next";
import { LoadingState } from "@/components/ui/loading-state";
import { Skeleton } from "@/components/ui/skeleton";
import { useLatestSnapshot } from "@/features/eventsub";
import { cn } from "@/lib/utils";

const CARD_CLASS = "rounded-lg border border-border bg-card p-4 mb-6";

export function QuotaCard() {
	const { t } = useTranslation();
	const { data, isLoading } = useLatestSnapshot();
	const snap = data?.snapshot;

	if (isLoading) return <QuotaCardSkeleton />;
	if (!snap) {
		return (
			<div className={cn(CARD_CLASS, "text-sm text-muted-foreground")}>
				{t("eventsub.no_snapshot_yet")}
			</div>
		);
	}

	const quotaPct =
		snap.max_total_cost > 0
			? Math.min(100, Math.round((snap.total_cost / snap.max_total_cost) * 100))
			: 0;

	return (
		<div className={CARD_CLASS}>
			<div className="flex items-center justify-between mb-3">
				<QuotaFigure
					label={t("eventsub.quota_label")}
					value={`${snap.total_cost} / ${snap.max_total_cost}`}
				/>
				<QuotaFigure
					label={t("eventsub.active_subs")}
					value={snap.total}
					className="text-right"
				/>
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
	);
}

export function QuotaCardSkeleton() {
	const { t } = useTranslation();
	return (
		<LoadingState className={CARD_CLASS}>
			<div className="flex items-center justify-between mb-3">
				<QuotaFigure
					label={t("eventsub.quota_label")}
					value={<Skeleton className="inline-block h-6 w-24 align-middle" />}
				/>
				<QuotaFigure
					label={t("eventsub.active_subs")}
					value={<Skeleton className="inline-block h-6 w-10 align-middle" />}
					className="text-right"
				/>
			</div>
			<Skeleton className="h-2 w-full rounded-full" />
			<div className="mt-2 flex h-4 items-center">
				<Skeleton className="h-3 w-48" />
			</div>
		</LoadingState>
	);
}

function QuotaFigure({
	label,
	value,
	className,
}: {
	label: string;
	value: ReactNode;
	className?: string;
}) {
	return (
		<div className={className}>
			<div className="text-sm text-muted-foreground">{label}</div>
			<div className="text-2xl font-medium">{value}</div>
		</div>
	);
}
