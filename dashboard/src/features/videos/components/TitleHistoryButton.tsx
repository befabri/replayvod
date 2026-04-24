import { ListBullets } from "@phosphor-icons/react";
import { useState } from "react";
import { useTranslation } from "react-i18next";
import {
	Dialog,
	DialogContent,
	DialogDescription,
	DialogHeader,
	DialogTitle,
	DialogTrigger,
} from "@/components/ui/dialog";
import { spanDurationLabel, useVideoTitles } from "@/features/videos";

// TitleHistoryButton surfaces the titles captured during a recording.
// A stream can change title mid-broadcast; the server polls Helix
// every title_tracking.interval_minutes and appends each distinct
// title to this video's history via the titles / video_titles M2M.
//
// The badge always renders for DONE recordings — we could suppress
// it for single-title cases but that would require eagerly fetching
// the count per card (N+1 on list pages) or denormalizing a
// title_count onto VideoResponse. Pragmatic: the dialog just shows
// one item in that case, which still surfaces the stream title as
// captured, and the query is lazy (enabled toggles on open) so list
// pages don't fan out N requests at render time.
export function TitleHistoryButton({
	videoId,
	children,
	className,
}: {
	videoId: number;
	children?: React.ReactNode;
	className?: string;
}) {
	const { t } = useTranslation();
	const [open, setOpen] = useState(false);
	const { data: titles, isLoading } = useVideoTitles(videoId, open);

	return (
		<Dialog open={open} onOpenChange={setOpen}>
			<DialogTrigger
				render={(triggerProps) => (
					<button
						type="button"
						{...triggerProps}
						onClick={(e) => {
							// The card wraps a Link; clicking the badge
							// must not navigate to /watch.
							e.preventDefault();
							e.stopPropagation();
							triggerProps.onClick?.(e);
						}}
						className={
							className ??
							"inline-flex items-center gap-1 rounded-md bg-overlay px-2 py-0.5 text-xs font-medium text-white transition-colors hover:bg-black/80"
						}
						aria-label={t("videos.title_history.tooltip")}
						title={t("videos.title_history.tooltip")}
					>
						{children ?? (
							<>
								<ListBullets className="size-3" />
								{t("videos.title_history.badge")}
							</>
						)}
					</button>
				)}
			/>
			<DialogContent className="max-w-lg">
				<DialogHeader>
					<DialogTitle>{t("videos.title_history.heading")}</DialogTitle>
					<DialogDescription>
						{t("videos.title_history.description")}
					</DialogDescription>
				</DialogHeader>

				{isLoading && (
					<div className="text-muted-foreground text-sm py-4">
						{t("common.loading")}
					</div>
				)}

				{titles && titles.length === 0 && (
					<div className="text-muted-foreground text-sm py-4">
						{t("videos.title_history.empty")}
					</div>
				)}

				{titles && titles.length > 0 && (
					<ol className="flex flex-col gap-2 py-2">
						{titles.map((title, idx) => (
							<li
								key={`${title.id}-${title.started_at}`}
								className="flex items-start gap-3 rounded-md bg-muted/50 px-3 py-2"
							>
								<span className="text-xs font-mono text-muted-foreground w-6 shrink-0 pt-0.5">
									{idx + 1}.
								</span>
								<div className="min-w-0 flex-1">
									<div className="text-sm leading-snug">{title.name}</div>
									<div className="text-xs text-muted-foreground">
										{spanDurationLabel(title.duration_seconds, t)}
									</div>
								</div>
							</li>
						))}
					</ol>
				)}
			</DialogContent>
		</Dialog>
	);
}
