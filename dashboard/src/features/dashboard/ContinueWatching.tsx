import { useMemo } from "react";
import { useTranslation } from "react-i18next";
import type { VideoResponse } from "@/api/generated/trpc";
import { ViewAllLink } from "@/components/ui/view-all-link";
import { usePlaybackSettings } from "@/features/settings/playback";
import { VideoCard } from "@/features/videos/components/VideoCard";
import { VideoGrid } from "@/features/videos/components/VideoGrid";
import { useCanManageVideos } from "@/features/videos/permissions";
import { useContinueWatching } from "@/features/videos/queries";
import { isContinueWatchingVideo } from "@/features/videos/resume-policy";

const CONTINUE_WATCHING_LIMIT = 5;

export function ContinueWatching() {
	const { data } = useContinueWatching(CONTINUE_WATCHING_LIMIT);
	if (!data?.length) return null;
	return <ContinueWatchingSection candidates={data} />;
}

function ContinueWatchingSection({
	candidates,
}: {
	candidates: VideoResponse[];
}) {
	const policy = usePlaybackSettings();
	const { t } = useTranslation();
	const canManage = useCanManageVideos();
	const videos = useMemo(
		() =>
			candidates
				.filter((video) => isContinueWatchingVideo(video, policy))
				.slice(0, CONTINUE_WATCHING_LIMIT),
		[candidates, policy],
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
