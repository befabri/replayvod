import { PencilSimpleIcon } from "@phosphor-icons/react";
import { Link } from "@tanstack/react-router";
import { useState } from "react";
import { useTranslation } from "react-i18next";
import { Avatar } from "@/components/ui/avatar";
import { Button } from "@/components/ui/button";
import {
	Dialog,
	DialogContent,
	DialogHeader,
	DialogTitle,
} from "@/components/ui/dialog";
import { QualityTag } from "@/components/ui/quality-tag";
import { ViewAllLink } from "@/components/ui/view-all-link";
import { useChannel } from "@/features/channels";
import type { ScheduleResponse } from "@/features/schedules";
import { EditForm } from "@/features/schedules/components/EditForm";
import { scheduleQualityLabel } from "@/features/schedules/quality";
import { useMineSchedules } from "@/features/schedules/queries";

export function ScheduleStatistics() {
	const { t } = useTranslation();
	const { data, isLoading, isError } = useMineSchedules();

	const items = data?.data.slice(0, 4) ?? [];
	const total = data?.data.length ?? 0;

	return (
		<div className="rounded-lg bg-card text-card-foreground p-4 shadow-sm sm:p-5">
			<div className="mb-4 flex items-center justify-between gap-3">
				<h5 className="text-xl font-medium text-foreground">
					{t("schedules.title")}
				</h5>
				{total > 0 ? <ViewAllLink to="/dashboard/schedules" /> : null}
			</div>
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
	const [editing, setEditing] = useState(false);
	const label = channel?.broadcaster_name ?? schedule.broadcaster_id;

	return (
		<li className="flex items-center gap-2 py-2 first:pt-0 last:pb-0 text-sm">
			<Avatar
				src={channel?.profile_image_url}
				name={label}
				alt={label}
				size="md"
			/>
			<Link
				to="/dashboard/channels/$channelId"
				params={{ channelId: schedule.broadcaster_id }}
				className="min-w-0 flex-1 truncate font-medium text-foreground hover:text-link"
			>
				{label}
			</Link>
			<QualityTag>{scheduleQualityLabel(t, schedule.quality)}</QualityTag>
			{schedule.is_disabled ? (
				<span className="rounded-sm bg-muted px-1.5 py-0.5 text-[10px] uppercase tracking-wide text-muted-foreground">
					{t("schedules.disabled")}
				</span>
			) : null}
			<Button
				variant="ghost"
				size="icon-sm"
				onClick={() => setEditing(true)}
				aria-label={t("schedules.edit")}
			>
				<PencilSimpleIcon />
			</Button>

			<Dialog open={editing} onOpenChange={setEditing}>
				<DialogContent className="max-w-xl">
					<DialogHeader>
						<DialogTitle>
							{t("schedules.edit_title")}
							<span className="ml-2 font-mono text-xs text-muted-foreground">
								{schedule.broadcaster_id}
							</span>
						</DialogTitle>
					</DialogHeader>
					<EditForm schedule={schedule} onDone={() => setEditing(false)} />
				</DialogContent>
			</Dialog>
		</li>
	);
}
