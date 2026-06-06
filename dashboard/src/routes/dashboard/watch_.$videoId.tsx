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
import {
	CategoryTimelineCard,
	TitleTimelineCard,
	VideoMetaGrid,
} from "@/features/videos/components/VideoDetails";
import { VideoInfo } from "@/features/videos/components/VideoInfo";
import { WatchPlayer } from "@/features/videos/components/WatchPlayer";
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
	const id = Number(videoId);
	const { data: video, isLoading, error } = useVideo(id);
	const timelineEnabled = !!video && video.status === "DONE";
	const { data: timelineEvents } = useMergedTimeline(id, timelineEnabled);
	const playlist = useMemo(
		() =>
			video ? buildRecordingPlaylist(video, timelineEvents, API_URL) : null,
		[video, timelineEvents],
	);
	const audioWaveformEnabled =
		!!playlist &&
		video?.status === "DONE" &&
		playlist.isAudioOnly &&
		playlist.parts.length > 0;
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

	if (video.status !== "DONE") {
		return (
			<TitledLayout title={video.title?.trim() || video.display_name}>
				<p className="text-muted-foreground">
					{translate("watch.not_ready", { status: video.status })}
				</p>
				<Link
					to="/dashboard/videos"
					search={{
						tab: "all",
						status: undefined,
						view: "grid",
						sort: "newest",
						quality: undefined,
						language: undefined,
						duration: undefined,
					}}
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
						/* Layout toggle docks alongside the title via the
						   VideoInfo headerAction slot. Hidden below xl —
						   the page always stacks there regardless of the
						   saved preference, so the button would be a no-op.
						   Icon-only with a tooltip for the verbose label. */
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
