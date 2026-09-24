import type * as React from "react";
import { cn } from "@/lib/utils";

export function Skeleton({ className, ...props }: React.ComponentProps<"div">) {
	return (
		<div
			data-slot="skeleton"
			className={cn("animate-pulse rounded-md bg-foreground/10", className)}
			{...props}
		/>
	);
}

export function SwitchSkeleton({ className }: { className?: string }) {
	return (
		<Skeleton className={cn("h-5 w-9 shrink-0 rounded-full", className)} />
	);
}

export function FieldSkeleton({ className }: { className?: string }) {
	return <Skeleton className={cn("h-9 w-full", className)} />;
}

export function ButtonSkeleton({ className }: { className?: string }) {
	return <Skeleton className={cn("h-9 w-28 shrink-0", className)} />;
}

export function BadgeSkeleton({ className }: { className?: string }) {
	return <Skeleton className={cn("h-5.5 w-20 shrink-0", className)} />;
}
