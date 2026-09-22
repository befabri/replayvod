import type * as React from "react";
import { useEffect, useState } from "react";

import { cn } from "@/lib/utils";

type Size = "sm" | "md" | "lg" | "xl" | "2xl" | "3xl";

const sizeClass: Record<Size, string> = {
	sm: "h-6 w-6 text-[10px]",
	md: "h-8 w-8 text-xs",
	lg: "h-10 w-10 text-sm",
	xl: "h-12 w-12 text-base",
	"2xl": "h-16 w-16 text-xl",
	"3xl": "h-24 w-24 text-2xl",
};

interface AvatarProps extends Omit<React.ComponentProps<"span">, "children"> {
	src?: string | null;
	alt?: string;
	name?: string;
	size?: Size;
	isLive?: boolean;
	liveRingClass?: string;
}

export function Avatar({
	src,
	alt: _alt,
	name,
	size = "md",
	isLive,
	liveRingClass = "ring-card",
	className,
	...props
}: AvatarProps) {
	const initials = (name || "")
		.split(" ")
		.map((p) => p.charAt(0))
		.slice(0, 2)
		.join("")
		.toUpperCase();

	const [errored, setErrored] = useState(false);
	// biome-ignore lint/correctness/useExhaustiveDependencies: src is the trigger, not a body dependency
	useEffect(() => {
		setErrored(false);
	}, [src]);

	const showImg = !!src && !errored;

	return (
		<span
			className={cn(
				"relative inline-flex items-center justify-center rounded-full bg-muted text-muted-foreground shrink-0",
				sizeClass[size],
				className,
			)}
			{...props}
		>
			{showImg ? (
				<img
					src={src}
					alt=""
					className="h-full w-full rounded-full object-cover"
					onError={() => setErrored(true)}
				/>
			) : initials ? (
				<span className="font-medium">{initials}</span>
			) : null}
			{isLive && (
				<span
					aria-hidden="true"
					className={cn(
						"absolute -bottom-0.5 -right-0.5 block rounded-full bg-destructive ring-2",
						size === "sm" && "h-2 w-2",
						(size === "md" || size === "lg") && "h-2.5 w-2.5",
						size === "xl" && "h-3 w-3",
						size === "2xl" && "h-3.5 w-3.5",
						size === "3xl" && "h-4 w-4",
						liveRingClass,
					)}
				/>
			)}
		</span>
	);
}
