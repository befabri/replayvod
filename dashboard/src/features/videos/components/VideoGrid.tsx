import { cn } from "@/lib/utils";

export const VIDEO_GRID_CLASS = {
	compact: "grid grid-cols-[repeat(auto-fit,minmax(320px,1fr))] gap-4",
	wide: "grid grid-cols-[repeat(auto-fit,minmax(400px,1fr))] gap-4",
} as const;

export const VIDEO_GRID_LAYOUT = {
	compact: { minItemWidth: 320, gap: 16, estimateRowHeight: 320 },
	wide: { minItemWidth: 400, gap: 16, estimateRowHeight: 340 },
} as const;

export type VideoGridVariant = keyof typeof VIDEO_GRID_CLASS;

export function VideoGrid({
	variant = "compact",
	className,
	children,
}: {
	variant?: VideoGridVariant;
	className?: string;
	children: React.ReactNode;
}) {
	return (
		<div className={cn(VIDEO_GRID_CLASS[variant], className)}>{children}</div>
	);
}
