import { useTranslation } from "react-i18next";

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
