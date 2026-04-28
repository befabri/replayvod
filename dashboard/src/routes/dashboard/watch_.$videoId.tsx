import { ArrowsIn, ArrowsOut } from "@phosphor-icons/react";
import { createFileRoute, Link } from "@tanstack/react-router";
import { useEffect, useRef, useState } from "react";
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
import { useVideo } from "@/features/videos";
import {
	CategoryTimelineCard,
	TitleTimelineCard,
	VideoMetaGrid,
} from "@/features/videos/components/VideoDetails";
import { VideoInfo } from "@/features/videos/components/VideoInfo";
import { WatchPlayer } from "@/features/videos/components/WatchPlayer";
import { cn } from "@/lib/utils";

// Layout choice persists across visits. "aside" places the timeline
// cards in a right column next to the player; "wide" stacks them below
// so the player can take the full content width. Only meaningful at
// xl+ where the aside layout is two-column — narrower viewports always
// stack, and the toggle is hidden there.
type WatchLayout = "aside" | "wide";
const LAYOUT_STORAGE_KEY = "watch:layout";

export const Route = createFileRoute("/dashboard/watch_/$videoId")({
	component: WatchPage,
});

function WatchPage() {
	const { t } = useTranslation();
	const { videoId } = Route.useParams();
	const id = Number(videoId);
	const { data: video, isLoading, error } = useVideo(id);

	const [layout, setLayout] = useState<WatchLayout>("aside");
	const hydratedRef = useRef(false);

	useEffect(() => {
		hydratedRef.current = true;
		const stored = window.localStorage.getItem(LAYOUT_STORAGE_KEY);
		if (stored === "aside" || stored === "wide") setLayout(stored);
	}, []);

	useEffect(() => {
		if (!hydratedRef.current) return;
		window.localStorage.setItem(LAYOUT_STORAGE_KEY, layout);
	}, [layout]);

	if (isLoading) {
		return <div className="text-muted-foreground">{t("common.loading")}</div>;
	}
	if (error) {
		return (
			<TitledLayout title={t("videos.failed_to_load")}>
				<div className="rounded-lg bg-destructive/10 p-4 text-destructive text-sm shadow-sm">
					{error.message}
				</div>
			</TitledLayout>
		);
	}
	if (!video) {
		return <div className="text-muted-foreground">{t("videos.not_found")}</div>;
	}

	if (video.status !== "DONE") {
		return (
			<TitledLayout title={video.title?.trim() || video.display_name}>
				<p className="text-muted-foreground">
					{t("watch.not_ready", { status: video.status })}
				</p>
				<Link
					// biome-ignore lint/suspicious/noExplicitAny: static route typing
					to={"/dashboard/videos" as any}
					className="inline-block mt-4 text-link hover:underline"
				>
					{t("watch.back_to_videos")}
				</Link>
			</TitledLayout>
		);
	}

	// credentials:"include" is set globally on the tRPC client but the
	// <video> element has no such override, so the browser uses its
	// default (same-origin). When API_URL is cross-origin, the streaming
	// endpoint must opt-in via CORS+Credentials — handled in middleware.
	const streamURL = `${API_URL}/api/v1/videos/${video.id}/stream`;

	const isWide = layout === "wide";

	return (
		<div
			className={cn(
				"grid gap-8",
				!isWide && "xl:grid-cols-[minmax(0,1fr)_360px]",
			)}
		>
			<div className="flex flex-col gap-6 min-w-0">
				<WatchPlayer
					src={streamURL}
					title={video.title?.trim() || video.display_name}
				/>
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
											onClick={() =>
												setLayout((w) => (w === "aside" ? "wide" : "aside"))
											}
											aria-label={
												isWide
													? t("watch.switch_to_aside")
													: t("watch.switch_to_wide")
											}
										>
											{isWide ? <ArrowsIn /> : <ArrowsOut />}
										</Button>
									}
								/>
								<TooltipContent>
									{isWide ? t("watch.layout_aside") : t("watch.layout_wide")}
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
