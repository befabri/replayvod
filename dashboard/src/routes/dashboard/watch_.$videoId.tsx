import { createFileRoute, Link } from "@tanstack/react-router";
import { useCallback, useEffect, useMemo } from "react";
import { useTranslation } from "react-i18next";
import type { TimelineEvent } from "@/api/generated/trpc";
import { TitledLayout } from "@/components/layout/titled-layout";
import { QueryBoundary } from "@/components/query-boundary";
import { buttonVariants } from "@/components/ui/button";
import { API_URL } from "@/env";
import { usePlaybackSettings } from "@/features/settings/playback";
import {
	isVideoId,
	prefetchVideo,
	useAudioWaveform,
	useCachedVideo,
	useInvalidateVideo,
	useResume,
	useVideoTimeline,
	useWatchProgressWriter,
} from "@/features/videos";
import { RelatedRecordings } from "@/features/videos/components/RelatedRecordings";
import { RemovedRecordingPanel } from "@/features/videos/components/RemovedRecordingPanel";
import { RemoveVideoButton } from "@/features/videos/components/RemoveVideoButton";
import {
	CategoryTimelineCard,
	TitleTimelineCard,
	VideoMetaGrid,
} from "@/features/videos/components/VideoDetails";
import { VideoInfo } from "@/features/videos/components/VideoInfo";
import {
	WatchHeaderActions,
	type WatchLayout,
} from "@/features/videos/components/WatchHeaderActions";
import { WatchPageGrid } from "@/features/videos/components/WatchPageGrid";
import { WatchPageSkeleton } from "@/features/videos/components/WatchPageSkeleton";
import { WatchPlayer } from "@/features/videos/components/WatchPlayer";
import { useCanManageVideos } from "@/features/videos/permissions";
import {
	buildRecordingPlaylist,
	isPlayableVideo,
} from "@/features/videos/playback";
import { useSuspenseVideo } from "@/features/videos/queries";
import { recordingPosterURL } from "@/features/videos/thumbnail";
import { useLocalStorageState } from "@/hooks/useLocalStorageState";
import { cn } from "@/lib/utils";

const NO_TIMELINE_EVENTS: TimelineEvent[] = [];
const LAYOUT_STORAGE_KEY = "watch:layout";
const parseWatchLayout = (raw: string): WatchLayout | null =>
	raw === "aside" || raw === "wide" ? raw : null;
const serializeWatchLayout = (value: WatchLayout) => value;

const VIDEOS_LIBRARY_SEARCH = {
	tab: "all",
	status: undefined,
	view: "grid",
	sort: "newest",
	quality: undefined,
	language: undefined,
	duration: undefined,
	source: undefined,
} as const;

export const Route = createFileRoute("/dashboard/watch_/$videoId")({
	validateSearch: (search: Record<string, unknown>) => ({
		t: parseSeekParam(search.t),
	}),
	loader: ({ context: { queryClient, trpc }, params, preload }) =>
		prefetchVideo(queryClient, trpc, Number(params.videoId), { preload }),
	component: WatchPage,
});

function WatchPage() {
	const { t } = useTranslation();
	const { videoId } = Route.useParams();
	const id = Number(videoId);
	if (!isVideoId(id)) {
		return <div className="text-muted-foreground">{t("videos.not_found")}</div>;
	}
	return (
		<div className="flex flex-col gap-6">
			<RelatedRecordings videoId={id} />
			<QueryBoundary key={id} fallback={<WatchContentLoading id={id} />}>
				<WatchContent id={id} />
			</QueryBoundary>
		</div>
	);
}

function useWatchLayout() {
	return useLocalStorageState<WatchLayout>(
		LAYOUT_STORAGE_KEY,
		"aside",
		parseWatchLayout,
		serializeWatchLayout,
	);
}

function WatchContentLoading({ id }: { id: number }) {
	const [layout] = useWatchLayout();
	const cached = useCachedVideo(id);
	return (
		<WatchPageSkeleton
			layout={layout}
			video={isPlayableVideo(cached) ? cached : undefined}
		/>
	);
}

function WatchContent({ id }: { id: number }) {
	const policy = usePlaybackSettings();
	const { t: translate } = useTranslation();
	const { t: initialOffsetSeconds } = Route.useSearch();
	const navigate = Route.useNavigate();
	const canManage = useCanManageVideos();
	const { data: video } = useSuspenseVideo(id);
	const playable = isPlayableVideo(video);
	const timelineEnabled = playable;
	const { data: timelineEvents = NO_TIMELINE_EVENTS } = useVideoTimeline(
		id,
		timelineEnabled,
	);
	const playlist = useMemo(
		() =>
			playable ? buildRecordingPlaylist(video, timelineEvents, API_URL) : null,
		[playable, video, timelineEvents],
	);
	const audioWaveformEnabled =
		!!playlist && playable && playlist.isAudioOnly && playlist.parts.length > 0;
	const thumbnailUrl = recordingPosterURL(video);
	const { data: audioWaveform, isFetching: isAudioWaveformFetching } =
		useAudioWaveform(id, audioWaveformEnabled);
	const writeWatchProgress = useWatchProgressWriter(id);
	const invalidateVideo = useInvalidateVideo(id);
	const handleMediaUnavailable = useCallback(() => {
		void invalidateVideo();
	}, [invalidateVideo]);

	const [layout, setLayout] = useWatchLayout();
	const resume = useResume(
		playable ? video : null,
		playlist?.totalDurationSeconds ?? 0,
		policy,
	);
	useEffect(() => {
		if (!resume.replay) return;
		writeWatchProgress(resume.replay.positionSeconds, resume.replay.completed);
	}, [resume.replay, writeWatchProgress]);

	if (video.deleted_at) {
		return (
			<TitledLayout title={video.title?.trim() || video.display_name}>
				<RemovedRecordingPanel video={video} canManage={canManage} />
			</TitledLayout>
		);
	}

	if (video.status !== "DONE") {
		return (
			<TitledLayout title={video.title?.trim() || video.display_name}>
				<p className="text-muted-foreground">
					{translate("watch.not_ready", { status: video.status })}
				</p>
				<Link
					to="/dashboard/videos"
					search={VIDEOS_LIBRARY_SEARCH}
					className={cn(
						buttonVariants({ variant: "link", size: "inline" }),
						"mt-4",
					)}
				>
					{translate("watch.back_to_videos")}
				</Link>
			</TitledLayout>
		);
	}

	const playerInitialOffsetSeconds =
		initialOffsetSeconds ?? resume.offsetSeconds;

	return (
		<WatchPageGrid
			layout={layout}
			main={
				<>
					{playlist && (
						<WatchPlayer
							key={playlist.videoId}
							playlist={playlist}
							initialOffsetSeconds={playerInitialOffsetSeconds}
							resumedFromSeconds={
								initialOffsetSeconds == null ? resume.offsetSeconds : undefined
							}
							onProgress={writeWatchProgress}
							onMediaUnavailable={handleMediaUnavailable}
							unavailableActions={
								<>
									{canManage ? (
										<RemoveVideoButton
											videoId={video.id}
											withLabel
											onRemoved={() =>
												navigate({
													to: "/dashboard/videos",
													search: VIDEOS_LIBRARY_SEARCH,
												})
											}
										/>
									) : null}
									<Link
										to="/dashboard/activity/history"
										search={{ outcome: "all", media: "removed" }}
										className={cn(
											buttonVariants({ variant: "link", size: "inline" }),
											"text-xs",
										)}
									>
										{translate("watch.view_history")}
									</Link>
								</>
							}
							thumbnailUrl={thumbnailUrl}
							audioWaveform={audioWaveform ?? null}
							audioWaveformLoading={
								audioWaveformEnabled &&
								isAudioWaveformFetching &&
								!audioWaveform?.peaks?.length
							}
						/>
					)}
					<VideoInfo
						video={video}
						headerAction={
							<WatchHeaderActions
								video={video}
								canManage={canManage}
								layout={layout}
								onLayoutChange={setLayout}
								onRemoved={() =>
									navigate({
										to: "/dashboard/videos",
										search: VIDEOS_LIBRARY_SEARCH,
									})
								}
							/>
						}
					/>
					<VideoMetaGrid video={video} />
				</>
			}
			aside={
				<>
					<CategoryTimelineCard video={video} />
					<TitleTimelineCard video={video} />
				</>
			}
		/>
	);
}

function parseSeekParam(raw: unknown): number | undefined {
	if (typeof raw !== "string" && typeof raw !== "number") return undefined;
	const n = Number(raw);
	return Number.isFinite(n) && n >= 0 ? n : undefined;
}
