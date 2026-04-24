import type * as React from "react";
import { cn } from "@/lib/utils";

export function PageTitle({
	children,
	className,
}: {
	children: React.ReactNode;
	className?: string;
}) {
	return (
		<h1
			className={cn(
				"text-3xl md:text-4xl font-heading font-bold text-foreground tracking-tight",
				className,
			)}
		>
			{children}
		</h1>
	);
}

export function TitledLayout({
	title,
	description,
	actions,
	children,
}: {
	title: React.ReactNode;
	description?: React.ReactNode;
	actions?: React.ReactNode;
	children: React.ReactNode;
}) {
	return (
		<>
			<div className="flex flex-col gap-4 pb-8 sm:flex-row sm:items-end sm:justify-between">
				<div className="min-w-0 space-y-2">
					<PageTitle>{title}</PageTitle>
					{description ? (
						<div className="text-sm text-muted-foreground">{description}</div>
					) : null}
				</div>
				{actions ? (
					<div className="flex flex-wrap items-center gap-3">{actions}</div>
				) : null}
			</div>
			{children}
		</>
	);
}
