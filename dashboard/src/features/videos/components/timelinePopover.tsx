import { useTranslation } from "react-i18next";
import { CategoryBoxArt } from "@/features/categories/components/CategoryBoxArt";
import { cn } from "@/lib/utils";

// Shared content for the timeline popovers used both in the watch-page scrubber
// and the dashboard's Running-now strip, so a part hover and a metadata-change
// hover read identically in either place.
//
// Tone adapts only the text colors to the surface the popover floats on: "video"
// sits over arbitrary frames (force white), "surface" sits on a themed popover
// (theme tokens). The structure is the same either way.
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
	// Shown only when neither a category nor a title resolved — keeps a popover
	// from rendering empty for an unusual change event.
	fallback?: string;
};

// TimelinePartContent is the part-segment popover body: which part, its media
// range, and its duration/size figures.
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

// TimelineChangeContent is the metadata-change popover body: the category (with
// its box art) and/or the title that changed. The box art carries the category
// visually; there are no color-coded dots.
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
