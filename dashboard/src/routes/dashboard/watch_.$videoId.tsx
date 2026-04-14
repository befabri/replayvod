import { ArrowsIn, ArrowsOut } from "@phosphor-icons/react";
import { createFileRoute, Link } from "@tanstack/react-router";
import { useEffect, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { TitledLayout } from "@/components/layout/titled-layout";
import { Button } from "@/components/ui/button";
import { API_URL } from "@/env";
import { useVideo } from "@/features/videos";
import { VideoDetails } from "@/features/videos/components/VideoDetails";
import { VideoInfo } from "@/features/videos/components/VideoInfo";
import { cn } from "@/lib/utils";

type WatchWidth = "full" | "contained";
const WIDTH_STORAGE_KEY = "watch:width";

export const Route = createFileRoute("/dashboard/watch_/$videoId")({
	component: WatchPage,
});

function WatchPage() {
	const { t } = useTranslation();
	const { videoId } = Route.useParams();
	const id = Number(videoId);
	const { data: video, isLoading, error } = useVideo(id);

	const [width, setWidth] = useState<WatchWidth>("full");
	const hydratedRef = useRef(false);

	// Hydrate from localStorage on mount only.
	useEffect(() => {
		hydratedRef.current = true;
		const stored = window.localStorage.getItem(WIDTH_STORAGE_KEY);
		if (stored === "contained" || stored === "full") setWidth(stored);
	}, []);

	// Persist on change, but only after initial hydration so we don't
	// write the just-read value back to storage on mount.
	useEffect(() => {
		if (!hydratedRef.current) return;
		window.localStorage.setItem(WIDTH_STORAGE_KEY, width);
	}, [width]);

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

	// Credentials:"include" is set globally on the tRPC client but the
	// <video> element has no such override, so the browser uses its default
	// (same-origin only). When API_URL is cross-origin, CORS+Credentials on
	// the streaming endpoint must be configured — handled in CORS middleware.
	const streamURL = `${API_URL}/api/v1/videos/${video.id}/stream`;

	return (
		<div className="flex flex-col gap-6">
			<div className="flex items-center justify-end">
				<Button
					variant="outline"
					size="sm"
					onClick={() => setWidth((w) => (w === "full" ? "contained" : "full"))}
					aria-label={
						width === "full"
							? t("watch.switch_to_contained")
							: t("watch.switch_to_full")
					}
				>
					{width === "full" ? <ArrowsIn /> : <ArrowsOut />}
					{width === "full"
						? t("watch.width_contained")
						: t("watch.width_full")}
				</Button>
			</div>

			<div
				className={cn(
					"rounded-lg overflow-hidden bg-black shadow-sm",
					width === "contained" && "max-w-5xl mx-auto w-full",
				)}
			>
				{/* biome-ignore lint/a11y/useMediaCaption: no captions available from Twitch VODs */}
				<video
					controls
					preload="metadata"
					className="w-full aspect-video"
					src={streamURL}
				/>
			</div>

			<div
				className={cn(
					"flex flex-col gap-8",
					width === "contained" && "max-w-5xl mx-auto w-full",
				)}
			>
				<VideoInfo video={video} />
				<VideoDetails video={video} />
			</div>
		</div>
	);
}
