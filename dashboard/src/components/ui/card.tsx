import type * as React from "react";

import { cn } from "@/lib/utils";

function Card({ className, ...props }: React.ComponentProps<"div">) {
	return (
		<div
			data-slot="card"
			className={cn(
				"rounded-xl border border-border bg-card text-card-foreground shadow-sm",
				className,
			)}
			{...props}
		/>
	);
}

function CardHeader({ className, ...props }: React.ComponentProps<"div">) {
	return (
		<div
			data-slot="card-header"
			className={cn("flex flex-col gap-1.5 p-4 pb-0", className)}
			{...props}
		/>
	);
}

function CardTitle({ className, ...props }: React.ComponentProps<"h3">) {
	return (
		<h3
			data-slot="card-title"
			className={cn("text-base font-semibold leading-tight", className)}
			{...props}
		/>
	);
}

function CardDescription({ className, ...props }: React.ComponentProps<"p">) {
	return (
		<p
			data-slot="card-description"
			className={cn("text-sm text-muted-foreground", className)}
			{...props}
		/>
	);
}

function CardStatusHeader({
	title,
	description,
	status,
	className,
}: {
	title: React.ReactNode;
	description: React.ReactNode;
	status: React.ReactNode;
	className?: string;
}) {
	return (
		<CardHeader className={cn("@container", className)}>
			<div className="grid gap-y-1.5 @sm:grid-cols-[minmax(0,1fr)_auto] @sm:items-center @sm:gap-x-4 @sm:gap-y-0">
				<CardTitle>{title}</CardTitle>
				<div className="flex @sm:justify-end">{status}</div>
				<CardDescription className="@sm:col-span-2">
					{description}
				</CardDescription>
			</div>
		</CardHeader>
	);
}

function CardContent({ className, ...props }: React.ComponentProps<"div">) {
	return (
		<div data-slot="card-content" className={cn("p-4", className)} {...props} />
	);
}

function CardFooter({ className, ...props }: React.ComponentProps<"div">) {
	return (
		<div
			data-slot="card-footer"
			className={cn("flex items-center p-4 pt-0", className)}
			{...props}
		/>
	);
}

export {
	Card,
	CardContent,
	CardDescription,
	CardFooter,
	CardHeader,
	CardStatusHeader,
	CardTitle,
};
