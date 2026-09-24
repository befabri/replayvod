import { cva, type VariantProps } from "class-variance-authority";
import type * as React from "react";

import { cn } from "@/lib/utils";

const alertVariants = cva(
	"group/alert flex w-full items-start gap-3 rounded-lg border px-4 py-3 text-sm [&>svg]:mt-0.5 [&>svg]:size-5 [&>svg]:shrink-0",
	{
		variants: {
			variant: {
				default:
					"border-border bg-muted/50 text-foreground [&>svg]:text-muted-foreground",
				success:
					"border-primary/20 bg-primary/10 text-foreground [&>svg]:text-primary",
				destructive: "border-destructive/30 bg-destructive/10 text-destructive",
			},
		},
		defaultVariants: { variant: "default" },
	},
);

function Alert({
	className,
	variant,
	icon,
	action,
	children,
	...props
}: React.ComponentProps<"div"> &
	VariantProps<typeof alertVariants> & {
		icon?: React.ReactNode;
		action?: React.ReactNode;
	}) {
	return (
		<div
			role={variant === "destructive" ? "alert" : "status"}
			data-slot="alert"
			data-variant={variant ?? "default"}
			className={cn(alertVariants({ variant }), className)}
			{...props}
		>
			{icon}
			<div className="min-w-0 flex-1">{children}</div>
			{action ? <div className="shrink-0 self-center">{action}</div> : null}
		</div>
	);
}

function AlertTitle({ className, ...props }: React.ComponentProps<"div">) {
	return (
		<div
			data-slot="alert-title"
			className={cn("font-medium", className)}
			{...props}
		/>
	);
}

function AlertDescription({
	className,
	...props
}: React.ComponentProps<"div">) {
	return (
		<div
			data-slot="alert-description"
			className={cn(
				"text-muted-foreground group-data-[variant=destructive]/alert:text-destructive/90",
				className,
			)}
			{...props}
		/>
	);
}

export { Alert, AlertDescription, AlertTitle, alertVariants };
