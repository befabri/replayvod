import { Pause, PencilSimple, Play } from "@phosphor-icons/react";
import { useState } from "react";
import { useTranslation } from "react-i18next";
import { toast } from "sonner";
import { Avatar } from "@/components/ui/avatar";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import {
	Dialog,
	DialogContent,
	DialogHeader,
	DialogTitle,
} from "@/components/ui/dialog";
import { useChannel } from "@/features/channels";
import type { ScheduleResponse } from "@/features/schedules";
import { useToggleSchedule } from "@/features/schedules/queries";
import { cn } from "@/lib/utils";
import { EditForm } from "./EditForm";

export function ScheduleRow({ schedule }: { schedule: ScheduleResponse }) {
	const { t } = useTranslation();
	const toggle = useToggleSchedule();
	const { data: channel } = useChannel(schedule.broadcaster_id);
	const [editing, setEditing] = useState(false);

	const channelLabel = channel?.broadcaster_name ?? schedule.broadcaster_id;

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
		<div
			className={cn(
				"rounded-lg bg-card p-4 shadow-sm border-l-4 transition-colors",
				schedule.is_disabled ? "border-muted-foreground/40" : "border-primary",
			)}
		>
			<div className="flex items-start justify-between gap-4">
				<div className="flex-1 min-w-0">
					<div className="flex items-center gap-2 mb-2 min-w-0">
						<Avatar
							src={channel?.profile_image_url}
							name={channelLabel}
							alt={channelLabel}
							size="sm"
						/>
						<div className="min-w-0 flex-1">
							<div className="text-sm font-medium truncate">{channelLabel}</div>
							<div className="font-mono text-xs text-muted-foreground truncate">
								{schedule.broadcaster_id}
							</div>
						</div>
					</div>
					<div className="flex flex-wrap items-center gap-2">
						<Badge variant="blue">{schedule.quality}</Badge>
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
					</div>
				</div>
				<div className="flex flex-col gap-1 items-end shrink-0">
					<Button
						variant="ghost"
						size="icon-sm"
						onClick={() => setEditing(true)}
						aria-label={t("schedules.edit")}
					>
						<PencilSimple />
					</Button>
					<Button
						variant="ghost"
						size="icon-sm"
						onClick={handleToggle}
						disabled={toggle.isPending}
						aria-label={
							schedule.is_disabled
								? t("schedules.enable")
								: t("schedules.disable")
						}
					>
						{schedule.is_disabled ? <Play weight="fill" /> : <Pause />}
					</Button>
				</div>
			</div>

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
