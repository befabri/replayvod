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
	actions,
	children,
}: {
	title: React.ReactNode;
	actions?: React.ReactNode;
	children: React.ReactNode;
}) {
	return (
		<>
			<div className="pb-8 flex items-end justify-between gap-4 flex-wrap">
				<PageTitle>{title}</PageTitle>
				{actions ? (
					<div className="flex items-center gap-3">{actions}</div>
				) : null}
			</div>
			{children}
		</>
	);
}
