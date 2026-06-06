import { ArrowsInIcon, ArrowsOutIcon } from "@phosphor-icons/react";
import { createFileRoute, Link } from "@tanstack/react-router";
import { useMemo } from "react";
import { useTranslation } from "react-i18next";
import { TitledLayout } from "@/components/layout/titled-layout";
import { Button } from "@/components/ui/button";
import {
	Tooltip,
	TooltipContent,
	TooltipProvider,
	TooltipTrigger,
} from "@/components/ui/tooltip";
import { API_URL } from "@/env";
import {
	useAudioWaveform,
	useMergedTimeline,
	useVideo,
} from "@/features/videos";
import { RemoveVideoButton } from "@/features/videos/components/RemoveVideoButton";
import {
	CategoryTimelineCard,
	TitleTimelineCard,
	VideoMetaGrid,
} from "@/features/videos/components/VideoDetails";
import { VideoInfo } from "@/features/videos/components/VideoInfo";
import { WatchPlayer } from "@/features/videos/components/WatchPlayer";
import { useCanManageVideos } from "@/features/videos/permissions";
import { buildRecordingPlaylist } from "@/features/videos/playback";
import { useLocalStorageState } from "@/hooks/useLocalStorageState";
import { cn } from "@/lib/utils";

// Layout choice persists across visits. "aside" places the timeline
// cards in a right column next to the player; "wide" stacks them below
// so the player can take the full content width. Only meaningful at
// xl+ where the aside layout is two-column — narrower viewports always
// stack, and the toggle is hidden there.
type WatchLayout = "aside" | "wide";
const LAYOUT_STORAGE_KEY = "watch:layout";
const parseWatchLayout = (raw: string): WatchLayout | null =>
	raw === "aside" || raw === "wide" ? raw : null;
const serializeWatchLayout = (value: WatchLayout) => value;

// Search state for the videos library, used whenever the watch page sends the
// user back there: the "not ready" exit link and the post-removal navigate.
// Mirrors the library route's validateSearch defaults so both entry points land
// on the same default view instead of an arbitrary one.
const VIDEOS_LIBRARY_SEARCH = {
	tab: "all",
	status: undefined,
	view: "grid",
	sort: "newest",
	quality: undefined,
	language: undefined,
	duration: undefined,
} as const;

export const Route = createFileRoute("/dashboard/watch_/$videoId")({
	validateSearch: (search: Record<string, unknown>) => ({
		t: parseSeekParam(search.t),
	}),
	component: WatchPage,
});

function WatchPage() {
	const { t: translate } = useTranslation();
	const { videoId } = Route.useParams();
	const { t: initialOffsetSeconds } = Route.useSearch();
	const navigate = Route.useNavigate();
	const canManage = useCanManageVideos();
	const id = Number(videoId);
	const { data: video, isLoading, error } = useVideo(id);
	const playable = !!video && video.status === "DONE" && !video.deleted_at;
	const timelineEnabled = playable;
	const { data: timelineEvents } = useMergedTimeline(id, timelineEnabled);
	const playlist = useMemo(
		() =>
			video && playable
				? buildRecordingPlaylist(video, timelineEvents, API_URL)
				: null,
		[playable, video, timelineEvents],
	);
	const audioWaveformEnabled =
		!!playlist && playable && playlist.isAudioOnly && playlist.parts.length > 0;
	const { data: audioWaveform, isFetching: isAudioWaveformFetching } =
		useAudioWaveform(id, audioWaveformEnabled);

	const [layout, setLayout] = useLocalStorageState<WatchLayout>(
		LAYOUT_STORAGE_KEY,
		"aside",
		parseWatchLayout,
		serializeWatchLayout,
	);

	if (isLoading) {
		return (
			<div className="text-muted-foreground">{translate("common.loading")}</div>
		);
	}
	if (error) {
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
				<p className="text-muted-foreground">{translate("watch.removed")}</p>
				<Link
					to="/dashboard/activity/history"
					search={{ filter: "removed" }}
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
						initialOffsetSeconds={initialOffsetSeconds}
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
							{/* Layout toggle docks alongside the title via the
							   VideoInfo headerAction slot. Hidden below xl —
							   the page always stacks there regardless of the
							   saved preference, so the button would be a no-op.
							   Icon-only with a tooltip for the verbose label. */}
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
