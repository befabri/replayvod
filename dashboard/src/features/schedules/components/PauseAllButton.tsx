import { PauseIcon, PlayIcon } from "@phosphor-icons/react";
import { useTranslation } from "react-i18next";
import { toast } from "sonner";
import { Button } from "@/components/ui/button";
import {
	useSchedulesPaused,
	useSetSchedulesPaused,
} from "@/features/schedules/queries";

// PauseAllButton toggles the global auto-download pause flag. Pausing leaves
// every schedule's own enabled/disabled state intact; resuming restores prior
// behavior exactly, since the flag lives on the server and never rewrites the
// individual schedules. Labelled "Pause all" / "Resume" (not "Resume all") to
// make clear resume restores prior state rather than enabling everything.
export function PauseAllButton() {
	const { t } = useTranslation();
	const { data } = useSchedulesPaused();
	const setPaused = useSetSchedulesPaused();
	const paused = data?.paused ?? false;

	const handleClick = async () => {
		try {
			await setPaused.mutateAsync({ paused: !paused });
		} catch (err) {
			toast.error(
				err instanceof Error ? err.message : t("schedules.pause_all_failed"),
			);
		}
	};

	return (
		<Button
			variant="outline"
			onClick={handleClick}
			disabled={setPaused.isPending}
		>
			{paused ? (
				<>
					<PlayIcon weight="fill" />
					{t("schedules.resume_all")}
				</>
			) : (
				<>
					<PauseIcon weight="fill" />
					{t("schedules.pause_all")}
				</>
			)}
		</Button>
	);
}
