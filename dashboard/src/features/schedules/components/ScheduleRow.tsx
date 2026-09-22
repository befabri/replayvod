import { GearSixIcon, PauseIcon, PlayIcon } from "@phosphor-icons/react";
import { useState } from "react";
import { useTranslation } from "react-i18next";
import { toast } from "sonner";
import { Avatar } from "@/components/ui/avatar";
import { Badge } from "@/components/ui/badge";
import {
	Dialog,
	DialogContent,
	DialogHeader,
	DialogTitle,
} from "@/components/ui/dialog";
import { QualityTag } from "@/components/ui/quality-tag";
import { useChannel } from "@/features/channels";
import type { ScheduleResponse } from "@/features/schedules";
import { scheduleQualityLabel } from "@/features/schedules/quality";
import { useToggleSchedule } from "@/features/schedules/queries";
import { scheduleRecordingTypeLabel } from "@/features/schedules/recording";
import { cn } from "@/lib/utils";
import { EditForm } from "./EditForm";

export function ScheduleRow({
	schedule,
	globallyPaused = false,
	canManage = true,
}: {
	schedule: ScheduleResponse;
	globallyPaused?: boolean;
	canManage?: boolean;
}) {
	const { t } = useTranslation();
	const toggle = useToggleSchedule();
	const { data: channel } = useChannel(schedule.broadcaster_id);
	const [editing, setEditing] = useState(false);

	const channelLabel = channel?.broadcaster_name ?? schedule.broadcaster_id;
	const dimmed = schedule.is_disabled || globallyPaused;

	const handleToggle = async () => {
		try {
			await toggle.mutateAsync({ id: schedule.id });
		} catch (err) {
			toast.error(
				err instanceof Error ? err.message : t("schedules.update_failed"),
			);
		}
	};

	return (
		<div className="relative flex overflow-hidden rounded-lg bg-card shadow-sm">
			<button
				type="button"
				onClick={handleToggle}
				disabled={!canManage || toggle.isPending || globallyPaused}
				aria-label={
					schedule.is_disabled ? t("schedules.enable") : t("schedules.disable")
				}
				className="peer/toggle relative min-w-0 flex-1 cursor-pointer text-left disabled:cursor-default"
			>
				<div className={cn("p-4 transition-opacity", dimmed && "opacity-50")}>
					<div className="flex items-center gap-3 mb-3 min-w-0">
						<Avatar
							src={channel?.profile_image_url}
							name={channelLabel}
							alt={channelLabel}
							size="md"
						/>
						<div className="truncate text-lg font-semibold">{channelLabel}</div>
					</div>
					<div className="flex flex-wrap items-center gap-2">
						<QualityTag>
							{schedule.recording_type === "audio"
								? scheduleRecordingTypeLabel(t, schedule.recording_type)
								: `${scheduleRecordingTypeLabel(t, schedule.recording_type)} · ${scheduleQualityLabel(t, schedule.quality)}`}
						</QualityTag>
						{schedule.recording_type !== "audio" && schedule.force_h264 && (
							<Badge variant="blue">{t("schedules.h264")}</Badge>
						)}
						{schedule.has_min_viewers && schedule.min_viewers != null && (
							<Badge variant="teal">
								{schedule.min_viewers} {t("schedules.min_viewers_unit")}
							</Badge>
						)}
						{schedule.has_categories && schedule.categories.length > 0 && (
							<Badge variant="purple">
								{schedule.categories.length} {t("schedules.categories_unit")}
							</Badge>
						)}
						{schedule.has_tags && schedule.tags.length > 0 && (
							<Badge variant="indigo">
								{schedule.tags.length} {t("schedules.tags_unit")}
							</Badge>
						)}
						{schedule.is_disabled ? (
							<Badge variant="muted">{t("schedules.disabled")}</Badge>
						) : (
							<Badge variant="green">{t("schedules.enabled")}</Badge>
						)}
					</div>
					<div className="mt-3 text-xs text-muted-foreground">
						{t("schedules.triggered_count", { count: schedule.trigger_count })}
						{schedule.last_triggered_at && (
							<>
								{" · "}
								{t("schedules.last_triggered")}:{" "}
								{new Date(schedule.last_triggered_at).toLocaleString()}
							</>
						)}
						{schedule.requested_from_name && (
							<>
								{" · "}
								{t("schedules.requested_by")}: {schedule.requested_from_name}
							</>
						)}
					</div>
				</div>
				<div
					aria-hidden="true"
					className={cn(
						"pointer-events-none absolute inset-x-0 bottom-0 h-1 transition-colors",
						dimmed ? "bg-muted-foreground/30" : "bg-badge-green-bg",
					)}
				/>
			</button>

			{canManage && (
				<button
					type="button"
					onClick={() => setEditing(true)}
					aria-label={t("schedules.edit")}
					className={cn(
						"flex w-[12.5%] shrink-0 cursor-pointer flex-col items-center justify-center gap-1.5 border-l border-foreground/10 text-muted-foreground transition-colors hover:text-foreground",
						dimmed && "opacity-50",
					)}
				>
					<GearSixIcon className="size-5" />
					<span className="text-xs">{t("schedules.edit")}</span>
				</button>
			)}

			{canManage && !globallyPaused && (
				<div className="pointer-events-none absolute inset-0 z-10 flex items-center justify-center text-foreground opacity-0 drop-shadow-md transition-opacity peer-hover/toggle:opacity-100 [&_svg]:size-8">
					{schedule.is_disabled ? (
						<PlayIcon weight="fill" />
					) : (
						<PauseIcon weight="fill" />
					)}
				</div>
			)}

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
		</div>
	);
}
