import { useCallback } from "react";
import { QueryBoundary } from "@/components/query-boundary";
import { VirtualGrid } from "@/components/ui/virtual-grid";
import type { VideoResponse } from "@/features/videos";
import { cn } from "@/lib/utils";
import { VideoCard } from "./VideoCard";
import { VIDEO_GRID_LAYOUT, type VideoGridVariant } from "./VideoGrid";
import { VideoGridLoading } from "./VideoGridLoading";

export function VirtualVideoGrid({
	videos,
	canManage,
	variant = "compact",
	className,
}: {
	videos: VideoResponse[];
	canManage: boolean;
	variant?: VideoGridVariant;
	className?: string;
}) {
	const layout = VIDEO_GRID_LAYOUT[variant];
	const getItemKey = useCallback((video: VideoResponse) => video.id, []);
	const renderItem = useCallback(
		(video: VideoResponse) => <VideoCard video={video} canManage={canManage} />,
		[canManage],
	);

	return (
		<QueryBoundary
			fallback={
				<VideoGridLoading variant={variant} className={cn("mt-0", className)} />
			}
		>
			<VirtualGrid
				items={videos}
				getItemKey={getItemKey}
				renderItem={renderItem}
				minItemWidth={layout.minItemWidth}
				estimateRowHeight={layout.estimateRowHeight}
				gap={layout.gap}
				className={className}
			/>
		</QueryBoundary>
	);
}
