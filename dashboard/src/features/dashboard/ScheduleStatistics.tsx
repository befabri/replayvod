import { Link } from "@tanstack/react-router";
import { useTranslation } from "react-i18next";
import { useChannel } from "@/features/channels";
import type { ScheduleResponse } from "@/features/schedules";
import { useMineSchedules } from "@/features/schedules/queries";

export function ScheduleStatistics() {
	const { t } = useTranslation();
	const { data, isLoading, isError } = useMineSchedules();

	const items = data?.data.slice(0, 4) ?? [];

	return (
		<div className="rounded-lg bg-card text-card-foreground p-4 shadow-sm sm:p-5">
			<h5 className="mb-4 text-xl font-medium text-foreground">
				{t("schedules.title")}
			</h5>
			{isLoading ? (
				<div className="text-muted-foreground">{t("common.loading")}</div>
			) : isError ? (
				<div className="text-destructive">{t("schedules.failed_to_load")}</div>
			) : items.length === 0 ? (
				<Link
					to="/dashboard/schedules"
					className="block text-sm text-muted-foreground hover:text-foreground"
				>
					{t("schedules.empty")}
				</Link>
			) : (
				<ul className="divide-y divide-border">
					{items.map((s) => (
						<ScheduleStatsRow key={s.id} schedule={s} />
					))}
				</ul>
			)}
		</div>
	);
}

// Extracted to its own component so each row can fire its own useChannel
// query for name/avatar resolution without cluttering the parent hook
// list. React Query dedupes across rows with the same broadcaster_id.
function ScheduleStatsRow({ schedule }: { schedule: ScheduleResponse }) {
	const { t } = useTranslation();
	const { data: channel } = useChannel(schedule.broadcaster_id);
	const label = channel?.broadcaster_name ?? schedule.broadcaster_id;

	return (
		<li className="flex items-center justify-between gap-3 py-2 first:pt-0 last:pb-0 text-sm">
			<div className="min-w-0 flex-1 truncate">
				<span className="font-medium text-foreground">{label}</span>
			</div>
			<div className="flex items-center gap-2 text-xs text-muted-foreground">
				<span>{schedule.quality}p</span>
				{schedule.is_disabled ? (
					<span className="rounded-sm bg-muted px-1.5 py-0.5 text-[10px] uppercase tracking-wide">
						{t("schedules.disabled")}
					</span>
				) : null}
			</div>
		</li>
	);
}
