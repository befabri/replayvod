import { StarIcon } from "@phosphor-icons/react";
import { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import { useSetChannelFavorite } from "@/features/channels";
import { cn } from "@/lib/utils";

export function ChannelFavoriteButton({
	broadcasterId,
	favorite,
	withLabel = false,
	className,
	onChanged,
}: {
	broadcasterId: string;
	favorite: boolean;
	withLabel?: boolean;
	className?: string;
	onChanged?: (favorite: boolean) => void;
}) {
	const { t } = useTranslation();
	const mutation = useSetChannelFavorite();
	const [optimistic, setOptimistic] = useState<boolean | null>(null);
	const active = optimistic ?? favorite;
	const label = active
		? t("channels.favorite.remove")
		: t("channels.favorite.add");

	// biome-ignore lint/correctness/useExhaustiveDependencies: reset optimistic state when the server-backed prop changes.
	useEffect(() => {
		setOptimistic(null);
	}, [favorite]);

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
					{ broadcaster_id: broadcasterId, favorite: next },
					{
						onSuccess: (state) => {
							setOptimistic(state.favorite);
							onChanged?.(state.favorite);
						},
						onError: () => {
							setOptimistic(null);
						},
					},
				);
			}}
			className={cn(
				withLabel
					? "inline-flex h-9 items-center gap-1.5 rounded-md border border-border bg-input/30 px-3 text-sm font-medium text-muted-foreground transition-colors hover:bg-input/50 hover:text-foreground disabled:pointer-events-none disabled:opacity-50"
					: "inline-flex size-8 shrink-0 items-center justify-center rounded-md text-muted-foreground transition-colors hover:bg-accent hover:text-foreground disabled:pointer-events-none disabled:opacity-50",
				active && "text-primary hover:text-primary",
				className,
			)}
		>
			<StarIcon className="size-4" weight={active ? "fill" : "regular"} />
			{withLabel ? label : null}
		</button>
	);
}
