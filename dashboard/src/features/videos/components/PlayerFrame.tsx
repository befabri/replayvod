import "./PlayerFrame.css";

import { useState } from "react";

export const VIDEO_PLAYER_FRAME =
	"rv-video-frame relative aspect-video w-full overflow-hidden bg-black shadow-sm";

export const AUDIO_PLAYER_CARD =
	"rv-watch-player-audio @container relative z-20 flex aspect-auto h-auto min-h-0 w-full flex-col items-stretch overflow-visible rounded-xl border border-border bg-card text-card-foreground shadow-sm";

export const AUDIO_PLAYER_BODY =
	"flex w-full min-w-0 items-stretch gap-4 px-4 pt-3 pb-[1.1rem] @max-[40rem]:px-3 @max-[40rem]:pt-[0.9rem]";

export function PlayerPoster({
	src,
	dismissed,
}: {
	src: string;
	dismissed?: boolean;
}) {
	const [failedSrc, setFailedSrc] = useState<string | null>(null);
	if (src === failedSrc) return null;

	return (
		<img
			src={src}
			alt=""
			decoding="sync"
			data-dismissed={dismissed || undefined}
			data-testid="player-poster"
			className="pointer-events-none absolute inset-0 z-1 size-full object-contain transition-opacity duration-200 ease-out data-dismissed:opacity-0"
			onError={() => setFailedSrc(src)}
		/>
	);
}

export function AudioThumbnail({ src }: { src: string }) {
	const [failedSrc, setFailedSrc] = useState<string | null>(null);
	if (src === failedSrc) return null;

	return (
		<div
			className="rv-audio-thumbnail relative hidden aspect-video flex-none self-center overflow-hidden rounded-[0.75rem] bg-[rgb(255_255_255/0.06)] @min-[50rem]:block"
			data-testid="audio-thumbnail"
		>
			<img
				src={src}
				alt=""
				className="absolute inset-0 h-full w-full object-cover"
				onError={() => setFailedSrc(src)}
			/>
		</div>
	);
}
