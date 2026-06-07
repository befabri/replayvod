import { BookmarkSimpleIcon } from "@phosphor-icons/react";
import { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import { useSetWatchLater } from "@/features/videos";
import { cn } from "@/lib/utils";

export function WatchLaterButton({
	videoId,
	watchLater,
	withLabel = false,
	className,
	onChanged,
}: {
	videoId: number;
	watchLater: boolean;
	withLabel?: boolean;
	className?: string;
	onChanged?: (watchLater: boolean) => void;
}) {
	const { t } = useTranslation();
	const mutation = useSetWatchLater();
	const [optimistic, setOptimistic] = useState<boolean | null>(null);
	const active = optimistic ?? watchLater;
	const label = active
		? t("videos.watch_later.remove")
		: t("videos.watch_later.add");

	// biome-ignore lint/correctness/useExhaustiveDependencies: reset optimistic state when the server-backed prop changes.
	useEffect(() => {
		setOptimistic(null);
	}, [watchLater]);

	return (
		<button
			type="button"
			aria-pressed={active}
			aria-label={label}
			title={label}
			disabled={mutation.isPending}
			onClick={(event) => {
				event.stopPropagation();
				event.preventDefault();
				const next = !active;
				setOptimistic(next);
				mutation.mutate(
					{ video_id: videoId, watch_later: next },
					{
						onSuccess: (state) => {
							setOptimistic(state.watch_later);
							onChanged?.(state.watch_later);
						},
						onError: () => {
							setOptimistic(null);
						},
					},
				);
			}}
			className={cn(
				withLabel
					? "inline-flex items-center gap-1.5 text-xs text-muted-foreground transition-colors hover:text-foreground"
					: "inline-flex size-7 shrink-0 items-center justify-center rounded-md text-muted-foreground transition-colors hover:bg-accent hover:text-foreground",
				active && "text-primary hover:text-primary",
				className,
			)}
		>
			<BookmarkSimpleIcon
				className="size-4"
				weight={active ? "fill" : "regular"}
			/>
			{withLabel ? label : null}
		</button>
	);
}
