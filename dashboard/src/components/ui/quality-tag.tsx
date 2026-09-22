import type * as React from "react";

import { cn } from "@/lib/utils";

export function QualityTag({
	children,
	className,
}: {
	children: React.ReactNode;
	className?: string;
}) {
	return (
		<span
			className={cn(
				"inline-flex items-center rounded-md border border-border/60 bg-primary/10 px-2 py-0.5 text-xs font-medium text-primary",
				className,
			)}
		>
			{children}
		</span>
	);
}
