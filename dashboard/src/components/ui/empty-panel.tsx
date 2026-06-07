import type * as React from "react";
import { cn } from "@/lib/utils";

// EmptyPanel is the subtle dashed-box placeholder shown when a list or
// tab has no rows (e.g. an empty Watch Later tab). For prominent,
// onboarding-style empties with an icon and a call to action, use
// EmptyState instead.
export function EmptyPanel({
	children,
	className,
}: {
	children: React.ReactNode;
	className?: string;
}) {
	return (
		<div
			className={cn(
				"rounded-xl border border-dashed border-border bg-card/50 px-6 py-12 text-center text-muted-foreground",
				className,
			)}
		>
			{children}
		</div>
	);
}
