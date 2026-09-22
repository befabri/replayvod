import { useTranslation } from "react-i18next";
import { CategoryBoxArt } from "@/features/categories/components/CategoryBoxArt";
import { cn } from "@/lib/utils";

export type TimelinePopoverTone = "video" | "surface";

const TONE: Record<
	TimelinePopoverTone,
	{ strong: string; muted: string; label: string }
> = {
	video: {
		strong: "text-white",
		muted: "text-white/55",
		label: "text-white/45",
	},
	surface: {
		strong: "text-foreground",
		muted: "text-muted-foreground",
		label: "text-muted-foreground",
	},
};

export type TimelineChangeData = {
	time?: string;
	category?: { name: string; boxArtUrl?: string | null };
	title?: { name: string };
	fallback?: string;
};

export function TimelinePartContent({
	heading,
	range,
	meta,
	tone,
}: {
	heading: string;
	range: string;
	meta?: string;
	tone: TimelinePopoverTone;
}) {
	const tones = TONE[tone];
	return (
		<div className="min-w-44 space-y-1">
			<div className={cn("font-medium", tones.strong)}>{heading}</div>
			<div className={cn("font-mono text-[11px]", tones.muted)}>{range}</div>
			{meta ? (
				<div className={cn("font-mono text-[11px]", tones.muted)}>{meta}</div>
			) : null}
		</div>
	);
}

export function TimelineChangeContent({
	change,
	tone,
}: {
	change: TimelineChangeData;
	tone: TimelinePopoverTone;
}) {
	const { t } = useTranslation();
	const tones = TONE[tone];
	const labelCls = cn(
		"text-[10px] font-medium uppercase tracking-[0.12em]",
		tones.label,
	);

	return (
		<div className="min-w-48 space-y-2">
			{change.time ? (
				<div className={cn("font-mono text-[11px]", tones.muted)}>
					{change.time}
				</div>
			) : null}
			{change.category ? (
				<div className="flex items-center gap-2">
					<CategoryBoxArt
						url={change.category.boxArtUrl}
						name={change.category.name}
						width={36}
						height={48}
						className="w-9 rounded-sm shrink-0"
					/>
					<div className="min-w-0">
						<div className={labelCls}>{t("watch.marker_category")}</div>
						<div className={cn("font-medium break-words", tones.strong)}>
							{change.category.name}
						</div>
					</div>
				</div>
			) : null}
			{change.title ? (
				<div className="min-w-0">
					<div className={labelCls}>{t("watch.marker_title")}</div>
					<div className={cn("font-medium break-words", tones.strong)}>
						{change.title.name}
					</div>
				</div>
			) : null}
			{!change.category && !change.title && change.fallback ? (
				<div className={cn("font-medium break-words", tones.strong)}>
					{change.fallback}
				</div>
			) : null}
		</div>
	);
}
