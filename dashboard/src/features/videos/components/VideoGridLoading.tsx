import { LoadingState } from "@/components/ui/loading-state";
import { cn } from "@/lib/utils";
import { VideoCardSkeleton } from "./VideoCardSkeleton";
import { VIDEO_GRID_CLASS, type VideoGridVariant } from "./VideoGrid";

export function VideoGridLoading({
	count = 3,
	className = "mt-4",
	variant = "compact",
}: {
	count?: number;
	className?: string;
	variant?: VideoGridVariant;
}) {
	return (
		<LoadingState className={cn(VIDEO_GRID_CLASS[variant], className)}>
			{Array.from({ length: count }, (_, index) => ({
				key: `video-grid-loading-${count}-${index}`,
				delay: `${index * 90}ms`,
			})).map((item) => (
				<div
					key={item.key}
					className="animate-in fade-in-0 slide-in-from-bottom-2 duration-300"
					style={{ animationDelay: item.delay }}
				>
					<VideoCardSkeleton />
				</div>
			))}
		</LoadingState>
	);
}
