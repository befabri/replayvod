import { CaretRight } from "@phosphor-icons/react";
import { createLink } from "@tanstack/react-router";
import type * as React from "react";
import { forwardRef } from "react";
import { useTranslation } from "react-i18next";

import { cn } from "@/lib/utils";

// ViewAllLink is the "VIEW ALL ›" affordance in card headers. It shares the
// mono-uppercase family with static header stats like "2 ACTIVE", so the only
// thing setting it apart is the trailing caret (and the hover brighten) — that
// caret is what marks it as a link rather than a label.
//
// Built through createLink so callers keep full type-safe `to`/`search`/`params`
// inference (a plain ComponentProps<typeof Link> wrapper collapses `search` to a
// reducer fn and rejects per-route object literals).
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
			<CaretRight
				weight="bold"
				className="size-3 transition-transform group-hover:translate-x-0.5"
			/>
		</a>
	);
});

export const ViewAllLink = createLink(ViewAllBase);
