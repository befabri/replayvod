import type * as React from "react";
import { cn } from "@/lib/utils";

export function EmptyState({
	icon,
	title,
	description,
	action,
	className,
}: {
	icon?: React.ReactNode;
	title: React.ReactNode;
	description?: React.ReactNode;
	action?: React.ReactNode;
	className?: string;
}) {
	return (
		<div
			className={cn(
				"flex flex-col items-center justify-center text-center rounded-lg bg-card text-card-foreground p-8 shadow-sm",
				className,
			)}
		>
			{icon ? (
				<div className="mb-3 text-muted-foreground [&_svg]:size-8">{icon}</div>
			) : null}
			<div className="text-base font-medium text-foreground">{title}</div>
			{description ? (
				<div className="mt-1 text-sm text-muted-foreground max-w-prose">
					{description}
				</div>
			) : null}
			{action ? <div className="mt-4">{action}</div> : null}
		</div>
	);
}
