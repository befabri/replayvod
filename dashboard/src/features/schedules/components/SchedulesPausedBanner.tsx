import { PauseIcon } from "@phosphor-icons/react";
import { useSelector } from "@tanstack/react-store";
import { useTranslation } from "react-i18next";
import { toast } from "sonner";
import { Button } from "@/components/ui/button";
import {
	useSchedulesPaused,
	useSetSchedulesPaused,
} from "@/features/schedules/queries";
import { authStore, hasRole } from "@/stores/auth";

// SchedulesPausedBanner surfaces the global pause state so it's obvious nothing
// will record. The text shows for everyone (viewers benefit from knowing why
// nothing records); the Resume action is admin-only, since schedule.setPaused is
// admin-only on the server. Renders nothing when not paused.
export function SchedulesPausedBanner() {
	const { t } = useTranslation();
	const { data } = useSchedulesPaused();
	const setPaused = useSetSchedulesPaused();
	const canManage = hasRole(
		useSelector(authStore, (s) => s.user),
		"admin",
	);

	if (!data?.paused) return null;

	const handleResume = async () => {
		try {
			await setPaused.mutateAsync({ paused: false });
		} catch (err) {
			toast.error(
				err instanceof Error ? err.message : t("schedules.pause_all_failed"),
			);
		}
	};

	return (
		<div className="mb-4 flex items-center gap-3 rounded-lg border border-border bg-muted/50 p-3 text-sm">
			<PauseIcon
				weight="fill"
				className="size-5 shrink-0 text-muted-foreground"
			/>
			<div className="flex-1">
				<div className="font-medium">{t("schedules.paused_banner_title")}</div>
				<div className="text-muted-foreground">
					{t("schedules.paused_banner_body")}
				</div>
			</div>
			{canManage && (
				<Button
					variant="outline"
					size="sm"
					onClick={handleResume}
					disabled={setPaused.isPending}
					className="shrink-0"
				>
					{t("schedules.resume_all")}
				</Button>
			)}
		</div>
	);
}
