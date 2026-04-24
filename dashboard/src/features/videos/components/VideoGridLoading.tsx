import { Skeleton } from "@/components/ui/skeleton";
import { cn } from "@/lib/utils";
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
		<div className={cn(VIDEO_GRID_CLASS[variant], className)}>
			{Array.from({ length: count }, (_, index) => ({
				key: `video-grid-loading-${count}-${index}`,
				delay: `${index * 90}ms`,
			})).map((item) => (
				<div
					key={item.key}
					className="overflow-hidden rounded-xl border border-border/60 bg-card/40 p-3 animate-in fade-in-0 slide-in-from-bottom-2 duration-300"
					style={{ animationDelay: item.delay }}
				>
					<Skeleton className="aspect-video w-full rounded-lg" />
					<div className="mt-3 flex items-start gap-3">
						<Skeleton className="size-10 shrink-0 rounded-full" />
						<div className="min-w-0 flex-1 space-y-2.5">
							<Skeleton className="h-4 w-[72%]" />
							<Skeleton className="h-3.5 w-[48%]" />
							<Skeleton className="h-3.5 w-[34%]" />
						</div>
					</div>
				</div>
			))}
		</div>
	);
}
