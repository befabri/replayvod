import type * as React from "react";
import { cn } from "@/lib/utils";
import { badgeVariants } from "./badge";
import { buttonVariants } from "./button";

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

export function ButtonSkeleton({
	className,
	children,
}: {
	className?: string;
	children?: React.ReactNode;
}) {
	if (children === undefined) {
		return <Skeleton className={cn("h-9 w-28 shrink-0", className)} />;
	}
	return (
		<Skeleton className={cn("shrink-0", className)}>
			<span aria-hidden="true" className={cn(buttonVariants(), "invisible")}>
				{children}
			</span>
		</Skeleton>
	);
}

export function BadgeSkeleton({
	className,
	children,
}: {
	className?: string;
	children?: React.ReactNode;
}) {
	if (children === undefined) {
		return <Skeleton className={cn("h-5.5 w-20 shrink-0", className)} />;
	}
	return (
		<Skeleton className={cn("shrink-0", className)}>
			<span aria-hidden="true" className={cn(badgeVariants(), "invisible")}>
				{children}
			</span>
		</Skeleton>
	);
}
