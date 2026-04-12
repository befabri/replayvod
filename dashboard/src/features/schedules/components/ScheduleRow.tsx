import { useState } from "react"
import { useTranslation } from "react-i18next"
import { Badge } from "@/components/ui/badge"
import type { ScheduleResponse } from "@/features/schedules"
import {
	useDeleteSchedule,
	useToggleSchedule,
} from "@/features/schedules/queries"
import { EditForm } from "./EditForm"

export function ScheduleRow({ schedule }: { schedule: ScheduleResponse }) {
	const { t } = useTranslation()
	const toggle = useToggleSchedule()
	const del = useDeleteSchedule()
	const [editing, setEditing] = useState(false)

	if (editing) {
		return <EditForm schedule={schedule} onDone={() => setEditing(false)} />
	}

	return (
		<div className="rounded-lg border border-border bg-card p-4">
			<div className="flex items-start justify-between gap-4">
				<div className="flex-1 min-w-0">
					<div className="font-mono text-sm text-muted-foreground mb-1">
						{schedule.broadcaster_id}
					</div>
					<div className="flex flex-wrap items-center gap-2 text-sm">
						<Badge>
							{t("videos.quality")}: {schedule.quality}
						</Badge>
						{schedule.has_min_viewers && schedule.min_viewers != null && (
							<Badge>
								{t("schedules.min_viewers")}: {schedule.min_viewers}
							</Badge>
						)}
						{schedule.has_categories && schedule.categories.length > 0 && (
							<Badge>
								{t("schedules.categories")}:{" "}
								{schedule.categories.map((c) => c.name).join(", ")}
							</Badge>
						)}
						{schedule.has_tags && schedule.tags.length > 0 && (
							<Badge>
								{t("schedules.tags")}:{" "}
								{schedule.tags.map((tag) => tag.name).join(", ")}
							</Badge>
						)}
						{schedule.is_disabled ? (
							<Badge variant="muted">{t("schedules.disabled")}</Badge>
						) : (
							<Badge variant="success">{t("schedules.enabled")}</Badge>
						)}
					</div>
					<div className="mt-2 text-xs text-muted-foreground">
						{t("schedules.triggered_count", { count: schedule.trigger_count })}
						{schedule.last_triggered_at && (
							<>
								{" · "}
								{t("schedules.last_triggered")}:{" "}
								{new Date(schedule.last_triggered_at).toLocaleString()}
							</>
						)}
					</div>
				</div>
				<div className="flex flex-col gap-2 items-end">
					<button
						type="button"
						onClick={() => setEditing(true)}
						className="text-sm px-3 py-1 rounded-md border border-border hover:bg-muted"
					>
						{t("schedules.edit")}
					</button>
					<button
						type="button"
						onClick={() => toggle.mutate({ id: schedule.id })}
						disabled={toggle.isPending}
						className="text-sm px-3 py-1 rounded-md border border-border hover:bg-muted disabled:opacity-60"
					>
						{schedule.is_disabled
							? t("schedules.enable")
							: t("schedules.disable")}
					</button>
					<button
						type="button"
						onClick={() => del.mutate({ id: schedule.id })}
						disabled={del.isPending}
						className="text-sm text-destructive hover:underline disabled:opacity-60"
					>
						{t("schedules.delete")}
					</button>
				</div>
			</div>
		</div>
	)
}
