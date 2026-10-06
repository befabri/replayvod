import { Link } from "@tanstack/react-router";
import type { ReactNode } from "react";
import { CategoryBoxArt } from "@/features/categories/components/CategoryBoxArt";
import type { VideoCategory, VideoTitle } from "@/features/videos";
import { formatPlaybackTime } from "@/features/videos/format";
import { cn } from "@/lib/utils";

export function TimelineRow({
	offsetSec,
	videoId,
	onNavigate,
	align = "center",
	children,
}: {
	offsetSec: number;
	videoId?: number;
	onNavigate?: () => void;
	align?: "center" | "baseline";
	children: ReactNode;
}) {
	const timestampClassName =
		"min-w-17 shrink-0 rounded-md bg-secondary px-2 py-1 text-center text-xs tabular-nums text-foreground/75";
	const timestamp = formatPlaybackTime(offsetSec);

	return (
		<li
			className={cn(
				"flex gap-3 px-5 py-3",
				align === "baseline" ? "items-baseline" : "items-center",
			)}
		>
			{videoId != null ? (
				<Link
					to="/dashboard/watch/$videoId"
					params={{ videoId: String(videoId) }}
					search={{ t: offsetSec }}
					onClick={onNavigate}
					className={cn(
						timestampClassName,
						"transition-colors hover:bg-accent hover:text-link",
					)}
				>
					{timestamp}
				</Link>
			) : (
				<span className={timestampClassName}>{timestamp}</span>
			)}
			{children}
		</li>
	);
}

export function CategoryEvent({
	category,
}: {
	category: Pick<VideoCategory, "id" | "name" | "box_art_url">;
}) {
	return (
		<Link
			to="/dashboard/categories/$categoryId"
			params={{ categoryId: category.id }}
			className="flex min-w-0 flex-1 items-center gap-3 rounded-md -mx-1 px-1 py-0.5 transition-colors hover:bg-accent/50"
		>
			<CategoryBoxArt
				url={category.box_art_url}
				name={category.name}
				width={40}
				height={54}
				className="w-10 rounded-md shrink-0"
			/>
			<span className="truncate text-sm font-medium">{category.name}</span>
		</Link>
	);
}

export function TitleEvent({ title }: { title: Pick<VideoTitle, "name"> }) {
	return (
		<div className="min-w-0 flex-1 text-sm leading-snug">{title.name}</div>
	);
}
