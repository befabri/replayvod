import type { ReactNode } from "react";
import { LoadingState } from "@/components/ui/loading-state";
import { Skeleton } from "@/components/ui/skeleton";
import { cn } from "@/lib/utils";

const NAME_WIDTHS = ["w-2/5", "w-1/2", "w-1/3", "w-2/5"];

export function AvatarRowsSkeleton({
	rows = 4,
	subtitle = false,
	trailing,
	rowClassName,
}: {
	rows?: number;
	subtitle?: boolean;
	trailing?: ReactNode;
	rowClassName?: string;
}) {
	return (
		<LoadingState className="divide-y divide-border">
			{Array.from({ length: rows }, (_, index) => (
				<div
					// biome-ignore lint/suspicious/noArrayIndexKey: skeleton rows are positional and never reorder.
					key={index}
					className={cn(
						"flex items-center py-2 first:pt-0 last:pb-0",
						rowClassName,
					)}
				>
					<Skeleton className="size-8 shrink-0 rounded-full" />
					<div className="min-w-0 flex-1">
						<div className={cn("flex items-center", subtitle ? "h-6" : "h-5")}>
							<Skeleton
								className={cn("h-3.5", NAME_WIDTHS[index % NAME_WIDTHS.length])}
							/>
						</div>
						{subtitle ? (
							<div className="flex h-4 items-center">
								<Skeleton className="h-3 w-3/5" />
							</div>
						) : null}
					</div>
					{trailing}
				</div>
			))}
		</LoadingState>
	);
}
