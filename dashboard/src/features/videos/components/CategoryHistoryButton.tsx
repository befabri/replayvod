import { GameController } from "@phosphor-icons/react";
import { Link } from "@tanstack/react-router";
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
import { CategoryBoxArt } from "@/features/categories/components/CategoryBoxArt";
import { useVideoCategories } from "@/features/videos";
import { formatDuration } from "@/features/videos/format";

// CategoryHistoryButton surfaces every category the stream was set
// to during this recording. Same render contract as TitleHistoryButton:
// badge on the card, dialog on click. Lazy-loaded on first open.
export function CategoryHistoryButton({
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
	const { data: categories, isLoading } = useVideoCategories(videoId, open);

	return (
		<Dialog open={open} onOpenChange={setOpen}>
			<DialogTrigger
				render={(triggerProps) => (
					<button
						type="button"
						{...triggerProps}
						onClick={(e) => {
							e.preventDefault();
							e.stopPropagation();
							triggerProps.onClick?.(e);
						}}
						className={
							className ??
							"inline-flex items-center gap-1 rounded-md bg-overlay px-2 py-0.5 text-xs font-medium text-white transition-colors hover:bg-black/80"
						}
						title={t("videos.categories_history.tooltip")}
					>
						{children ?? (
							<>
								<GameController className="size-3" />
								{t("videos.categories_history.badge")}
							</>
						)}
					</button>
				)}
			/>
			<DialogContent className="max-w-lg">
				<DialogHeader>
					<DialogTitle>{t("videos.categories_history.heading")}</DialogTitle>
					<DialogDescription>
						{t("videos.categories_history.description")}
					</DialogDescription>
				</DialogHeader>

				{isLoading && (
					<div className="text-muted-foreground text-sm py-4">
						{t("common.loading")}
					</div>
				)}

				{categories && categories.length === 0 && (
					<div className="text-muted-foreground text-sm py-4">
						{t("videos.categories_history.empty")}
					</div>
				)}

				{categories && categories.length > 0 && (
					<ul className="flex flex-col gap-2 py-2">
						{categories.map((c) => (
							<li key={`${c.id}-${c.started_at}`}>
								<Link
									// biome-ignore lint/suspicious/noExplicitAny: param route typing
									to={"/dashboard/categories/$categoryId" as any}
									// biome-ignore lint/suspicious/noExplicitAny: param route typing
									params={{ categoryId: c.id } as any}
									className="flex items-center gap-3 rounded-md bg-muted/50 px-3 py-2 hover:bg-accent transition-colors duration-75"
								>
									<CategoryBoxArt
										url={c.box_art_url}
										name={c.name}
										width={36}
										height={48}
										className="w-9 rounded-sm"
									/>
									<div className="min-w-0 flex-1">
										<div className="truncate text-sm font-medium">{c.name}</div>
										<div className="text-xs text-muted-foreground">
											{c.duration_seconds <= 0
												? t("videos.categories_history.just_switched")
												: c.duration_seconds < 60
													? t("videos.categories_history.less_than_minute")
													: formatDuration(c.duration_seconds)}
										</div>
									</div>
								</Link>
							</li>
						))}
					</ul>
				)}
			</DialogContent>
		</Dialog>
	);
}
