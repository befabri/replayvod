import { Link } from "@tanstack/react-router";
import { useTranslation } from "react-i18next";
import type { VideoResponse } from "@/api/generated/trpc";
import { buttonVariants } from "@/components/ui/button";
import { videoRemovalState } from "@/features/videos/removal";
import { cn } from "@/lib/utils";
import { VideoRemovalActions } from "./VideoRemovalActions";

export function RemovedRecordingPanel({
	video,
	canManage,
}: {
	video: VideoResponse;
	canManage: boolean;
}) {
	const { t } = useTranslation();
	const missing = video.deletion_kind === "missing";
	return (
		<>
			<p className="text-muted-foreground">
				{t(missing ? "watch.removed_missing" : "watch.removed")}
			</p>
			{missing && canManage ? (
				<div
					className="mt-4 flex flex-wrap items-center gap-4 text-sm"
					data-testid="removed-missing-actions"
				>
					{videoRemovalState(video) === "restorable" && (
						<span className="text-muted-foreground">
							{t("watch.removed_missing_actions")}
						</span>
					)}
					<VideoRemovalActions video={video} withLabel />
				</div>
			) : null}
			<Link
				to="/dashboard/activity/history"
				search={{ outcome: "all", media: "removed" }}
				className={cn(
					buttonVariants({ variant: "link", size: "inline" }),
					"mt-4",
				)}
			>
				{t("watch.back_to_history")}
			</Link>
		</>
	);
}
