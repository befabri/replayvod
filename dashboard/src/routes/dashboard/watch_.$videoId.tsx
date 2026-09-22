import { ArrowsInIcon, ArrowsOutIcon } from "@phosphor-icons/react";
import { createFileRoute, Link } from "@tanstack/react-router";
import { useCallback, useEffect, useMemo } from "react";
import { useTranslation } from "react-i18next";
import type { TimelineEvent } from "@/api/generated/trpc";
import { TitledLayout } from "@/components/layout/titled-layout";
import { Button } from "@/components/ui/button";
import {
	Tooltip,
	TooltipContent,
	TooltipProvider,
	TooltipTrigger,
} from "@/components/ui/tooltip";
import { API_URL } from "@/env";
import { usePlaybackSettings } from "@/features/settings/playback";
import {
	useAudioWaveform,
	useInvalidateVideo,
	useResume,
	useVideo,
	useVideoTimeline,
	useWatchProgressWriter,
} from "@/features/videos";
import { RelatedRecordings } from "@/features/videos/components/RelatedRecordings";
import { RemoveVideoButton } from "@/features/videos/components/RemoveVideoButton";
import {
	CategoryTimelineCard,
	TitleTimelineCard,
	VideoMetaGrid,
} from "@/features/videos/components/VideoDetails";
import { VideoInfo } from "@/features/videos/components/VideoInfo";
import { VideoRemovalActions } from "@/features/videos/components/VideoRemovalActions";
import { WatchLaterButton } from "@/features/videos/components/WatchLaterButton";
import { WatchPlayer } from "@/features/videos/components/WatchPlayer";
import { useCanManageVideos } from "@/features/videos/permissions";
import { buildRecordingPlaylist } from "@/features/videos/playback";
import { videoRemovalState } from "@/features/videos/removal";
import { localThumbnailURL } from "@/features/videos/thumbnail";
import { useLocalStorageState } from "@/hooks/useLocalStorageState";
import { cn } from "@/lib/utils";

type WatchLayout = "aside" | "wide";
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
	component: WatchPage,
});

function WatchPage() {
	const { videoId } = Route.useParams();
	const id = Number(videoId);
	return (
		<div className="flex flex-col gap-6">
			<RelatedRecordings videoId={id} />
			<WatchContent id={id} />
		</div>
	);
}

function WatchContent({ id }: { id: number }) {
	const policy = usePlaybackSettings();
	const { t: translate } = useTranslation();
	const { t: initialOffsetSeconds } = Route.useSearch();
	const navigate = Route.useNavigate();
	const canManage = useCanManageVideos();
	const { data: video, isLoading, error } = useVideo(id);
	const playable = !!video && video.status === "DONE" && !video.deleted_at;
	const timelineEnabled = playable;
	const { data: timelineEvents = NO_TIMELINE_EVENTS } = useVideoTimeline(
		id,
		timelineEnabled,
	);
	const playlist = useMemo(
		() =>
			video && playable
				? buildRecordingPlaylist(video, timelineEvents, API_URL)
				: null,
		[playable, video, timelineEvents],
	);
	const audioWaveformEnabled =
		!!playlist && playable && playlist.isAudioOnly && playlist.parts.length > 0;
	const thumbnailUrl = video?.thumbnail
		? localThumbnailURL(video.thumbnail)
		: null;
	const { data: audioWaveform, isFetching: isAudioWaveformFetching } =
		useAudioWaveform(id, audioWaveformEnabled);
	const writeWatchProgress = useWatchProgressWriter(id);
	const invalidateVideo = useInvalidateVideo(id);
	const handleMediaUnavailable = useCallback(() => {
		void invalidateVideo();
	}, [invalidateVideo]);

	const [layout, setLayout] = useLocalStorageState<WatchLayout>(
		LAYOUT_STORAGE_KEY,
		"aside",
		parseWatchLayout,
		serializeWatchLayout,
	);
	const resume = useResume(
		playable ? video : null,
		playlist?.totalDurationSeconds ?? 0,
		policy,
	);
	useEffect(() => {
		if (!resume.replay) return;
		writeWatchProgress(resume.replay.positionSeconds, resume.replay.completed);
	}, [resume.replay, writeWatchProgress]);

	if (isLoading) {
		return (
			<div className="text-muted-foreground">{translate("common.loading")}</div>
		);
	}
	if (error && !video) {
		return (
			<TitledLayout title={translate("videos.failed_to_load")}>
				<div className="rounded-lg bg-destructive/10 p-4 text-destructive text-sm shadow-sm">
					{error.message}
				</div>
			</TitledLayout>
		);
	}
	if (!video) {
		return (
			<div className="text-muted-foreground">
				{translate("videos.not_found")}
			</div>
		);
	}

	if (video.deleted_at) {
		return (
			<TitledLayout title={video.title?.trim() || video.display_name}>
				<p className="text-muted-foreground">
					{translate(
						video.deletion_kind === "missing"
							? "watch.removed_missing"
							: "watch.removed",
					)}
				</p>
				{video.deletion_kind === "missing" && canManage ? (
					<div
						className="mt-4 flex flex-wrap items-center gap-4 text-sm"
						data-testid="removed-missing-actions"
					>
						{videoRemovalState(video) === "restorable" && (
							<span className="text-muted-foreground">
								{translate("watch.removed_missing_actions")}
							</span>
						)}
						<VideoRemovalActions video={video} withLabel />
					</div>
				) : null}
				<Link
					to="/dashboard/activity/history"
					search={{ outcome: "all", media: "removed" }}
					className="inline-block mt-4 text-link hover:underline"
				>
					{translate("watch.back_to_history")}
				</Link>
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
					className="inline-block mt-4 text-link hover:underline"
				>
					{translate("watch.back_to_videos")}
				</Link>
			</TitledLayout>
		);
	}

	const isWide = layout === "wide";
	const playerInitialOffsetSeconds =
		initialOffsetSeconds ?? resume.offsetSeconds;

	return (
		<div
			className={cn(
				"grid gap-8",
				!isWide && "xl:grid-cols-[minmax(0,1fr)_360px]",
			)}
		>
			<div className="flex flex-col gap-6 min-w-0">
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
									className="text-xs text-link hover:underline"
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
						<div className="flex items-center gap-2">
							<WatchLaterButton
								videoId={video.id}
								watchLater={video.user_state?.watch_later ?? false}
								withLabel
							/>
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
							<TooltipProvider>
								<Tooltip>
									<TooltipTrigger
										render={
											<Button
												variant="outline"
												size="icon-sm"
												className="hidden xl:inline-flex"
												onClick={() => setLayout(isWide ? "aside" : "wide")}
												aria-label={
													isWide
														? translate("watch.switch_to_aside")
														: translate("watch.switch_to_wide")
												}
											>
												{isWide ? <ArrowsInIcon /> : <ArrowsOutIcon />}
											</Button>
										}
									/>
									<TooltipContent>
										{isWide
											? translate("watch.layout_aside")
											: translate("watch.layout_wide")}
									</TooltipContent>
								</Tooltip>
							</TooltipProvider>
						</div>
					}
				/>
				<VideoMetaGrid video={video} />
			</div>

			<aside className="min-w-0 flex flex-col gap-6">
				<CategoryTimelineCard video={video} />
				<TitleTimelineCard video={video} />
			</aside>
		</div>
	);
}

function parseSeekParam(raw: unknown): number | undefined {
	if (typeof raw !== "string" && typeof raw !== "number") return undefined;
	const n = Number(raw);
	return Number.isFinite(n) && n >= 0 ? n : undefined;
}
