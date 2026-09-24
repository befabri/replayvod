import type { VideoResponse } from "@/api/generated/trpc";
import { LoadingState } from "@/components/ui/loading-state";
import { Skeleton } from "@/components/ui/skeleton";
import { recordingPosterURL } from "@/features/videos/thumbnail";
import { cn } from "@/lib/utils";
import {
	AUDIO_PLAYER_BODY,
	AUDIO_PLAYER_CARD,
	AudioThumbnail,
	PlayerPoster,
	VIDEO_PLAYER_FRAME,
} from "./PlayerFrame";
import type { WatchLayout } from "./WatchHeaderActions";
import { WatchPageGrid } from "./WatchPageGrid";

const BADGE_WIDTHS = ["w-14", "w-10", "w-24", "w-16", "w-20", "w-28"];

export function WatchPageSkeleton({
	layout,
	video,
}: {
	layout: WatchLayout;
	video?: VideoResponse;
}) {
	const poster = video ? recordingPosterURL(video) : null;
	return (
		<LoadingState>
			<WatchPageGrid
				layout={layout}
				main={
					<>
						{video?.is_audio_only ? (
							<AudioPlayerSkeleton poster={poster} />
						) : (
							<VideoPlayerSkeleton poster={poster} />
						)}
						<div className="flex flex-col">
							<div className="flex flex-col gap-3 lg:flex-row lg:items-start lg:justify-between">
								<Skeleton className="h-7 w-2/3" />
								<div className="flex shrink-0 items-center gap-2">
									<Skeleton className="h-8 w-32" />
									<Skeleton className="size-8" />
								</div>
							</div>
							<div className="mt-3 flex flex-wrap items-center gap-2">
								{BADGE_WIDTHS.map((width) => (
									<Skeleton key={width} className={cn("h-5", width)} />
								))}
							</div>
							<div className="mt-5 flex items-center gap-3 border-y border-foreground/10 py-4">
								<Skeleton className="size-10 shrink-0 rounded-full" />
								<div className="space-y-1.5">
									<Skeleton className="h-4 w-32" />
									<Skeleton className="h-3 w-20" />
								</div>
							</div>
						</div>
						<Skeleton className="h-36 w-full rounded-xl" />
					</>
				}
				aside={
					<>
						<Skeleton className="h-56 w-full rounded-xl" />
						<Skeleton className="h-44 w-full rounded-xl" />
					</>
				}
			/>
		</LoadingState>
	);
}

function VideoPlayerSkeleton({ poster }: { poster: string | null }) {
	if (!poster) {
		return <Skeleton className="aspect-video w-full rounded-lg" />;
	}
	return (
		<div className={VIDEO_PLAYER_FRAME}>
			<PlayerPoster src={poster} />
		</div>
	);
}

function AudioPlayerSkeleton({ poster }: { poster: string | null }) {
	return (
		<div className={AUDIO_PLAYER_CARD}>
			<div className={AUDIO_PLAYER_BODY}>
				{poster ? <AudioThumbnail src={poster} /> : null}
				<div className="flex min-w-0 flex-auto flex-col">
					<div className="flex items-center justify-between gap-4 pb-[0.4rem]">
						<div className="flex items-center gap-[0.55rem]">
							<Skeleton className="size-10" />
							<Skeleton className="size-10 rounded-full" />
							<Skeleton className="size-10" />
							<Skeleton className="h-4 w-24" />
						</div>
						<div className="flex items-center gap-[0.55rem]">
							<Skeleton className="h-10 w-13" />
							<Skeleton className="h-4 w-28" />
						</div>
					</div>
					<div className="mt-auto pt-2">
						<Skeleton className="h-24 w-full" />
					</div>
				</div>
			</div>
		</div>
	);
}
