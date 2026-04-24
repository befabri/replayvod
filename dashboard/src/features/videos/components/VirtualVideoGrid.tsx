import { useCallback } from "react";
import { VirtualGrid } from "@/components/ui/virtual-grid";
import type { VideoResponse } from "@/features/videos";
import { VideoCard } from "./VideoCard";
import { VIDEO_GRID_LAYOUT, type VideoGridVariant } from "./VideoGrid";

export function VirtualVideoGrid({
	videos,
	variant = "compact",
	className,
}: {
	videos: VideoResponse[];
	variant?: VideoGridVariant;
	className?: string;
}) {
	const layout = VIDEO_GRID_LAYOUT[variant];
	const getItemKey = useCallback((video: VideoResponse) => video.id, []);
	const renderItem = useCallback(
		(video: VideoResponse) => <VideoCard video={video} />,
		[],
	);

	return (
		<VirtualGrid
			items={videos}
			getItemKey={getItemKey}
			renderItem={renderItem}
			minItemWidth={layout.minItemWidth}
			estimateRowHeight={layout.estimateRowHeight}
			gap={layout.gap}
			className={className}
		/>
	);
}
