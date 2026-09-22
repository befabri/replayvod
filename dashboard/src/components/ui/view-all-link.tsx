import { CaretRightIcon } from "@phosphor-icons/react";
import { createLink } from "@tanstack/react-router";
import type * as React from "react";
import { forwardRef } from "react";
import { useTranslation } from "react-i18next";

import { cn } from "@/lib/utils";

const ViewAllBase = forwardRef<
	HTMLAnchorElement,
	React.AnchorHTMLAttributes<HTMLAnchorElement>
>(({ className, children: _children, ...props }, ref) => {
	const { t } = useTranslation();
	return (
		<a
			ref={ref}
			{...props}
			className={cn(
				"group inline-flex shrink-0 items-center gap-1 font-mono text-xs text-muted-foreground transition-colors hover:text-foreground",
				className,
			)}
		>
			{t("common.view_all")}
			<CaretRightIcon
				weight="bold"
				className="size-3 transition-transform group-hover:translate-x-0.5"
			/>
		</a>
	);
});

export const ViewAllLink = createLink(ViewAllBase);
