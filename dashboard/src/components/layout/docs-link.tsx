import { ArrowUpRightIcon, BookOpenIcon } from "@phosphor-icons/react";
import type { ReactNode } from "react";
import { useTranslation } from "react-i18next";
import { cn } from "@/lib/utils";

export function DocsLink({
	page = "",
	children,
	compact = false,
	className,
}: {
	page?: "" | "schedules/" | "access/" | "access/#manage-the-whitelist";
	children?: ReactNode;
	compact?: boolean;
	className?: string;
}) {
	const { t } = useTranslation();
	return (
		<a
			href={`https://replayvod.com/docs/${page}`}
			target="_blank"
			rel="noopener noreferrer"
			title={compact ? t("docs.title") : undefined}
			className={cn(
				"inline-flex items-center gap-2 rounded-md text-sm text-muted-foreground transition-colors hover:text-link focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring",
				className,
			)}
		>
			<BookOpenIcon size={18} className="shrink-0" aria-hidden="true" />
			<span className={compact ? "sr-only" : undefined}>
				{children ?? t("docs.title")}
			</span>
			{!compact && <ArrowUpRightIcon size={14} aria-hidden="true" />}
			<span className="sr-only"> ({t("docs.new_tab")})</span>
		</a>
	);
}
