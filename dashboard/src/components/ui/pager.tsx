import { useTranslation } from "react-i18next";
import { Button } from "@/components/ui/button";
import { cn } from "@/lib/utils";

// Pager is the shared prev/next table paginator: a Previous/Next pair with a
// "Page N · M total" label. Works for offset tables (caller derives hasNext from
// total) and cursor tables (caller passes hasNext from the query). total is
// optional and only rendered when known.
export function Pager({
	page,
	onPrev,
	onNext,
	hasNext,
	total,
	className,
}: {
	page: number; // zero-based
	onPrev: () => void;
	onNext: () => void;
	hasNext: boolean;
	total?: number;
	className?: string;
}) {
	const { t } = useTranslation();
	return (
		<div className={cn("mt-4 flex items-center gap-2", className)}>
			<Button
				variant="outline"
				size="sm"
				disabled={page === 0}
				onClick={onPrev}
			>
				{t("common.previous")}
			</Button>
			<span className="text-sm text-muted-foreground tabular-nums">
				{t("common.page", { n: page + 1 })}
				{total !== undefined ? ` · ${t("common.total_count", { total })}` : ""}
			</span>
			<Button variant="outline" size="sm" disabled={!hasNext} onClick={onNext}>
				{t("common.next")}
			</Button>
		</div>
	);
}
