import { useMemo } from "react";
import { useTranslation } from "react-i18next";
import { ViewAllLink } from "@/components/ui/view-all-link";
import { usePlaybackSettings } from "@/features/settings/playback";
import { VideoCard } from "@/features/videos/components/VideoCard";
import { VideoGrid } from "@/features/videos/components/VideoGrid";
import { useCanManageVideos } from "@/features/videos/permissions";
import { useContinueWatching } from "@/features/videos/queries";
import { isContinueWatchingVideo } from "@/features/videos/resume-policy";

const CONTINUE_WATCHING_LIMIT = 5;

// ContinueWatching puts the recordings the viewer is partway through on the
// home page, most recently watched first. The server uses the same eligibility
// and order as the library tab; the local filter also handles patched progress.
export function ContinueWatching() {
	const policy = usePlaybackSettings();
	const { t } = useTranslation();
	const canManage = useCanManageVideos();
	const { data } = useContinueWatching(CONTINUE_WATCHING_LIMIT);
	const videos = useMemo(
		() =>
			(data ?? [])
				.filter((video) => isContinueWatchingVideo(video, policy))
				.slice(0, CONTINUE_WATCHING_LIMIT),
		[data, policy],
	);
	if (videos.length === 0) return null;
	return (
		<section
			aria-labelledby="continue-watching-heading"
			className="mb-6"
			data-testid="continue-watching"
		>
			<div className="mb-3 flex items-center justify-between gap-4">
				<h2
					id="continue-watching-heading"
					className="text-xl font-medium text-foreground"
				>
					{t("dashboard.continue_watching")}
				</h2>
				<ViewAllLink
					to="/dashboard/videos"
					search={{
						tab: "continue_watching",
						view: "grid",
						sort: "recently_watched",
						status: undefined,
						quality: undefined,
						language: undefined,
						duration: undefined,
						source: undefined,
					}}
				/>
			</div>
			<VideoGrid variant="compact">
				{videos.map((video) => (
					<VideoCard key={video.id} video={video} canManage={canManage} />
				))}
			</VideoGrid>
		</section>
	);
}
