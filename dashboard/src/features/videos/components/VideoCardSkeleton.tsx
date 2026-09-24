import { Skeleton } from "@/components/ui/skeleton";

export function VideoCardSkeleton() {
	return (
		<div className="flex flex-col">
			<Skeleton className="aspect-video w-full rounded-xl" />
			<div className="flex flex-col gap-2 px-3.5 pt-2 pb-3.5">
				<div className="flex h-8 items-start">
					<div className="flex h-5.5 min-w-0 flex-1 items-center">
						<Skeleton className="h-4 w-3/4" />
					</div>
				</div>
				<div className="flex items-center gap-2.5">
					<Skeleton className="size-6 shrink-0 rounded-full" />
					<div className="min-w-0 flex-1">
						<div className="flex h-5 items-center">
							<Skeleton className="h-3.5 w-28" />
						</div>
						<div className="flex h-4 items-center">
							<Skeleton className="h-3 w-40" />
						</div>
					</div>
				</div>
			</div>
		</div>
	);
}
