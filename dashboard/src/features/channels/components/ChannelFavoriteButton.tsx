import { StarIcon } from "@phosphor-icons/react";
import { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import { Toggle } from "@/components/ui/toggle";
import { useSetChannelFavorite } from "@/features/channels";

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
	const label = t("channels.favorite.label");
	const hint = active
		? t("channels.favorite.remove")
		: t("channels.favorite.add");

	// biome-ignore lint/correctness/useExhaustiveDependencies: reset optimistic state when the server-backed prop changes.
	useEffect(() => {
		setOptimistic(null);
	}, [favorite]);

	return (
		<Toggle
			variant={withLabel ? "outline" : "ghost"}
			size={withLabel ? "default" : "icon-sm"}
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
			className={className}
		>
			<StarIcon weight={active ? "fill" : "regular"} />
			{withLabel ? label : null}
		</Toggle>
	);
}
