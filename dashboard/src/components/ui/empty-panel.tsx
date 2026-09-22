import type * as React from "react";
import { cn } from "@/lib/utils";

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
