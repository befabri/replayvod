import { useTranslation } from "react-i18next";
import { Alert } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { ViewAllLink } from "@/components/ui/view-all-link";
import { VideoCard } from "@/features/videos/components/VideoCard";
import { VideoGrid } from "@/features/videos/components/VideoGrid";
import { VideoGridLoading } from "@/features/videos/components/VideoGridLoading";
import { useCanManageVideos } from "@/features/videos/permissions";
import { isPlayableVideo } from "@/features/videos/playback";
import { useInfiniteVideoPages } from "@/features/videos/queries";

const LATEST_RECORDINGS_SHOWN = 5;

export function LatestRecordings() {
	const { t } = useTranslation();
	const canManage = useCanManageVideos();
	const { data, isLoading, error, refetch, isFetching } = useInfiniteVideoPages(
		LATEST_RECORDINGS_SHOWN,
		"DONE",
		"created_at",
		"desc",
	);
	const videos = (data?.pages[0]?.items ?? [])
		.filter(isPlayableVideo)
		.slice(0, LATEST_RECORDINGS_SHOWN);
	if (videos.length === 0 && !isLoading && !error) return null;

	return (
		<section
			aria-labelledby="latest-recordings-heading"
			className="mb-6"
			data-testid="latest-recordings"
		>
			<LatestRecordingsHeading />
			{isLoading && (
				<VideoGridLoading count={LATEST_RECORDINGS_SHOWN} className="mt-0" />
			)}
			{error && (
				<Alert
					variant="destructive"
					className="mb-3"
					action={
						<Button
							variant="outline"
							size="sm"
							disabled={isFetching}
							onClick={() => void refetch()}
						>
							{t("common.retry")}
						</Button>
					}
				>
					{t("videos.failed_to_load")}
				</Alert>
			)}
			{videos.length > 0 && (
				<VideoGrid variant="compact">
					{videos.map((video) => (
						<VideoCard key={video.id} video={video} canManage={canManage} />
					))}
				</VideoGrid>
			)}
		</section>
	);
}

export function LatestRecordingsSkeleton() {
	return (
		<div className="mb-6">
			<LatestRecordingsHeading />
			<VideoGridLoading count={LATEST_RECORDINGS_SHOWN} className="mt-0" />
		</div>
	);
}

function LatestRecordingsHeading() {
	const { t } = useTranslation();
	return (
		<div className="mb-3 flex items-center justify-between gap-4">
			<h2
				id="latest-recordings-heading"
				className="text-xl font-medium text-foreground"
			>
				{t("dashboard.latest_recordings")}
			</h2>
			<ViewAllLink
				to="/dashboard/videos"
				search={{
					tab: "all",
					view: "grid",
					sort: "newest",
					status: "DONE",
					quality: undefined,
					language: undefined,
					duration: undefined,
					source: undefined,
				}}
			/>
		</div>
	);
}
