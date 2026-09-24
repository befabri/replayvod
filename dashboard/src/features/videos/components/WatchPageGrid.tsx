import type { ReactNode } from "react";
import { cn } from "@/lib/utils";
import type { WatchLayout } from "./WatchHeaderActions";

export function WatchPageGrid({
	layout,
	main,
	aside,
}: {
	layout: WatchLayout;
	main: ReactNode;
	aside: ReactNode;
}) {
	return (
		<div
			className={cn(
				"grid gap-8",
				layout === "aside" && "xl:grid-cols-[minmax(0,1fr)_360px]",
			)}
		>
			<div className="flex min-w-0 flex-col gap-6">{main}</div>
			<aside className="flex min-w-0 flex-col gap-6">{aside}</aside>
		</div>
	);
}
