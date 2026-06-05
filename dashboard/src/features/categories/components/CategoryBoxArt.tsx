import { GameControllerIcon } from "@phosphor-icons/react";
import { useEffect, useState } from "react";

import { resolveBoxArtSrcSet, resolveBoxArtUrl } from "@/lib/twitch";
import { cn } from "@/lib/utils";

// CategoryBoxArt resolves the Twitch box-art template URL and degrades
// gracefully: missing URL, template-replace miss, or network/404 all
// land on the same bg-muted placeholder with a controller icon.
export function CategoryBoxArt({
	url,
	name,
	width = 144,
	height = 192,
	sizes,
	decorative = false,
	placeholderIconSize = 32,
	className,
}: {
	url?: string | null;
	name: string;
	width?: number;
	height?: number;
	sizes?: string;
	decorative?: boolean;
	placeholderIconSize?: number;
	className?: string;
}) {
	const resolved = resolveBoxArtUrl(url, width, height);
	const srcSet = resolveBoxArtSrcSet(url, width, height);
	const imageSizes = srcSet ? (sizes ?? `${width}px`) : undefined;
	const [errored, setErrored] = useState(false);
	// biome-ignore lint/correctness/useExhaustiveDependencies: resolved is the reset trigger (the URL changed), not read in the effect body — auto-removing it would stop the error state from clearing on a new src.
	useEffect(() => {
		setErrored(false);
	}, [resolved]);

	const showImg = !!resolved && !errored;

	return (
		<div
			className={cn(
				"aspect-[3/4] overflow-hidden bg-muted flex items-center justify-center text-muted-foreground",
				className,
			)}
		>
			{showImg ? (
				<img
					src={resolved}
					srcSet={srcSet}
					sizes={imageSizes}
					alt={decorative ? "" : name}
					width={width}
					height={height}
					className="w-full h-full object-cover"
					loading="lazy"
					decoding="async"
					onError={() => setErrored(true)}
				/>
			) : (
				<GameControllerIcon size={placeholderIconSize} weight="duotone" />
			)}
		</div>
	);
}
