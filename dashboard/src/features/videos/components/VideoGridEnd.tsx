import { useTranslation } from "react-i18next";

// VideoGridEnd renders the "end of list" divider at the tail of an
// infinite-scroll grid. Callers pass an i18n key (not a resolved
// string) so the translation lookup stays co-located with the
// component — route code doesn't need to touch t() for this.
export function VideoGridEnd({
	labelKey = "videos.end_of_list",
}: {
	labelKey?: string;
}) {
	const { t } = useTranslation();
	return (
		<div className="mt-6 flex items-center gap-3 text-xs text-muted-foreground/80">
			<div className="h-px flex-1 bg-border/60" />
			<span className="whitespace-nowrap uppercase tracking-[0.18em]">
				{t(labelKey)}
			</span>
			<div className="h-px flex-1 bg-border/60" />
		</div>
	);
}
