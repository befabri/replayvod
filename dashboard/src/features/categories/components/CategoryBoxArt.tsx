import { GameController } from "@phosphor-icons/react";
import { useEffect, useState } from "react";

import { resolveBoxArtUrl } from "@/lib/twitch";
import { cn } from "@/lib/utils";

// CategoryBoxArt resolves the Twitch box-art template URL and degrades
// gracefully: missing URL, template-replace miss, or network/404 all
// land on the same bg-muted placeholder with a controller icon.
export function CategoryBoxArt({
	url,
	name,
	width = 144,
	height = 192,
	className,
}: {
	url?: string | null;
	name: string;
	width?: number;
	height?: number;
	className?: string;
}) {
	const resolved = resolveBoxArtUrl(url, width, height);
	const [errored, setErrored] = useState(false);
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
					alt={name}
					className="w-full h-full object-cover"
					loading="lazy"
					onError={() => setErrored(true)}
				/>
			) : (
				<GameController size={32} weight="duotone" />
			)}
		</div>
	);
}
