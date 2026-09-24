import { ArrowsInIcon, ArrowsOutIcon } from "@phosphor-icons/react";
import { useTranslation } from "react-i18next";
import type { VideoResponse } from "@/api/generated/trpc";
import { Button } from "@/components/ui/button";
import {
	Tooltip,
	TooltipContent,
	TooltipProvider,
	TooltipTrigger,
} from "@/components/ui/tooltip";
import { RemoveVideoButton } from "./RemoveVideoButton";
import { WatchLaterButton } from "./WatchLaterButton";

export type WatchLayout = "aside" | "wide";

export function WatchHeaderActions({
	video,
	canManage,
	layout,
	onLayoutChange,
	onRemoved,
}: {
	video: VideoResponse;
	canManage: boolean;
	layout: WatchLayout;
	onLayoutChange: (layout: WatchLayout) => void;
	onRemoved: () => void;
}) {
	const { t } = useTranslation();
	const isWide = layout === "wide";
	return (
		<div className="flex items-center gap-2">
			<WatchLaterButton
				videoId={video.id}
				watchLater={video.user_state?.watch_later ?? false}
				withLabel
			/>
			{canManage ? (
				<RemoveVideoButton videoId={video.id} withLabel onRemoved={onRemoved} />
			) : null}
			<TooltipProvider>
				<Tooltip>
					<TooltipTrigger
						render={
							<Button
								variant="outline"
								size="icon-sm"
								className="hidden xl:inline-flex"
								onClick={() => onLayoutChange(isWide ? "aside" : "wide")}
								aria-label={
									isWide
										? t("watch.switch_to_aside")
										: t("watch.switch_to_wide")
								}
							>
								{isWide ? <ArrowsInIcon /> : <ArrowsOutIcon />}
							</Button>
						}
					/>
					<TooltipContent>
						{isWide ? t("watch.layout_aside") : t("watch.layout_wide")}
					</TooltipContent>
				</Tooltip>
			</TooltipProvider>
		</div>
	);
}
