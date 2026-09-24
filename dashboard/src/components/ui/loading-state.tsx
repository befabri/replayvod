import { CircleNotchIcon } from "@phosphor-icons/react";
import type * as React from "react";
import { useTranslation } from "react-i18next";

import { cn } from "@/lib/utils";

export function LoadingState({
	label,
	className,
	children,
	...props
}: React.ComponentProps<"div"> & { label?: string }) {
	const { t } = useTranslation();
	const text = label ?? t("common.loading");
	if (children) {
		return (
			<div
				role="status"
				aria-label={text}
				data-slot="loading-state"
				className={className}
				{...props}
			>
				{children}
			</div>
		);
	}
	return (
		<div
			role="status"
			data-slot="loading-state"
			className={cn(
				"flex items-center gap-2 text-sm text-muted-foreground",
				className,
			)}
			{...props}
		>
			<CircleNotchIcon
				aria-hidden="true"
				className="size-4 shrink-0 motion-safe:animate-spin"
			/>
			<span>{text}</span>
		</div>
	);
}
