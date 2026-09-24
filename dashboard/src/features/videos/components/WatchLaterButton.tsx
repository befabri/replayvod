import { BookmarkSimpleIcon } from "@phosphor-icons/react";
import { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import { Toggle } from "@/components/ui/toggle";
import { useSetWatchLater } from "@/features/videos";

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
	const label = t("videos.watch_later.label");
	const hint = active
		? t("videos.watch_later.remove")
		: t("videos.watch_later.add");

	// biome-ignore lint/correctness/useExhaustiveDependencies: reset optimistic state when the server-backed prop changes.
	useEffect(() => {
		setOptimistic(null);
	}, [watchLater]);

	return (
		<Toggle
			variant={withLabel ? "outline" : "ghost"}
			size={withLabel ? "sm" : "icon-sm"}
			pressed={active}
			aria-label={label}
			title={hint}
			onClick={(event) => {
				event.stopPropagation();
				event.preventDefault();
				if (mutation.isPending) return;
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
			className={className}
		>
			<BookmarkSimpleIcon weight={active ? "fill" : "regular"} />
			{withLabel ? label : null}
		</Toggle>
	);
}
