import { useMemo } from "react";
import { useTranslation } from "react-i18next";
import { usePlaybackSettings } from "@/features/settings/playback";
import { VideoCard } from "@/features/videos/components/VideoCard";
import { VideoGrid } from "@/features/videos/components/VideoGrid";
import { useCanManageVideos } from "@/features/videos/permissions";
import { useContinueWatching } from "@/features/videos/queries";
import { resumeOffsetSeconds } from "@/features/videos/resume-policy";

const CONTINUE_WATCHING_FETCH = 12;
const CONTINUE_WATCHING_SHOWN = 6;

// ContinueWatching puts the recordings the viewer is partway through on the
// home page, most recently watched first. The server lists what was started
// and not played out; the resume policy trims what would open at the start
// anyway, so the strip only shows cards that actually resume.
export function ContinueWatching() {
	const policy = usePlaybackSettings();
	const { t } = useTranslation();
	const canManage = useCanManageVideos();
	const { data } = useContinueWatching(CONTINUE_WATCHING_FETCH);
	const videos = useMemo(
		() =>
			(data ?? [])
				.filter(
					(video) =>
						resumeOffsetSeconds(
							video.user_state,
							video.duration_seconds ?? 0,
							policy,
						) != null,
				)
				.slice(0, CONTINUE_WATCHING_SHOWN),
		[data, policy],
	);
	if (videos.length === 0) return null;
	return (
		<section
			aria-labelledby="continue-watching-heading"
			className="mb-6"
			data-testid="continue-watching"
		>
			<h2
				id="continue-watching-heading"
				className="mb-3 text-xl font-medium text-foreground"
			>
				{t("dashboard.continue_watching")}
			</h2>
			<VideoGrid variant="compact">
				{videos.map((video) => (
					<VideoCard key={video.id} video={video} canManage={canManage} />
				))}
			</VideoGrid>
		</section>
	);
}
