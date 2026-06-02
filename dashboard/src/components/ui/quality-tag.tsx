import type * as React from "react";

import { cn } from "@/lib/utils";

// QualityTag renders a recording-quality pill in the same accent style as the
// quality overlay on video thumbnails (text-primary, hairline border) so the
// quality reads consistently across the videos grid and the schedule lists.
// The thumbnail overlay in VideoCard keeps its own backdrop-blur variant since
// it sits on top of an image; on a flat card surface a faint primary tint
// stands in for that translucency.
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
