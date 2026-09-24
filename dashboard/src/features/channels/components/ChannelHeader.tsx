import { DownloadIcon, TwitchLogoIcon } from "@phosphor-icons/react";
import { useTranslation } from "react-i18next";
import type { ChannelResponse } from "@/api/generated/trpc";
import { Avatar } from "@/components/ui/avatar";
import { Button, buttonVariants } from "@/components/ui/button";
import { LoadingState } from "@/components/ui/loading-state";
import { Skeleton } from "@/components/ui/skeleton";
import { ChannelFavoriteButton } from "@/features/channels/components/ChannelFavoriteButton";
import { ChannelDownloadDialog } from "@/features/videos/components/ChannelDownloadDialog";

const HEADER_CLASS = "mt-4 flex gap-6 items-start mb-8";
const ACTIONS_CLASS = "flex items-center gap-2 shrink-0";

export function ChannelHeader({
	channel,
	isLive,
	canDownload,
}: {
	channel: ChannelResponse;
	isLive: boolean;
	canDownload: boolean;
}) {
	const { t } = useTranslation();
	return (
		<div className={HEADER_CLASS}>
			<Avatar
				src={channel.profile_image_url}
				name={channel.broadcaster_name}
				alt={channel.broadcaster_name}
				size="3xl"
				isLive={isLive}
				liveRingClass="ring-background"
			/>
			<div className="flex-1 min-w-0">
				<div className="text-muted-foreground mt-0.5">
					@{channel.broadcaster_login}
				</div>
				{channel.description && (
					<p className="text-sm mt-3 max-w-2xl">{channel.description}</p>
				)}
			</div>
			<div className={ACTIONS_CLASS}>
				<ChannelFavoriteButton
					broadcasterId={channel.broadcaster_id}
					favorite={channel.user_state?.favorite ?? false}
					withLabel
				/>
				<a
					href={`https://twitch.tv/${channel.broadcaster_login}`}
					target="_blank"
					rel="noopener noreferrer"
					className={buttonVariants({ variant: "outline" })}
				>
					<TwitchLogoIcon weight="fill" />
					{t("channels.open_in_twitch")}
				</a>
				{canDownload && (
					<ChannelDownloadDialog
						broadcasterId={channel.broadcaster_id}
						broadcasterName={channel.broadcaster_name}
						broadcasterLogin={channel.broadcaster_login}
						profileImageUrl={channel.profile_image_url}
						isLive={isLive}
					>
						<Button variant="outline">
							<DownloadIcon weight="regular" />
							{t("videos.trigger_download")}
						</Button>
					</ChannelDownloadDialog>
				)}
			</div>
		</div>
	);
}

export function ChannelHeaderSkeleton({
	canDownload,
}: {
	canDownload: boolean;
}) {
	return (
		<LoadingState className={HEADER_CLASS}>
			<Skeleton className="size-24 shrink-0 rounded-full" />
			<div className="flex-1 min-w-0">
				<Skeleton className="mt-1.5 h-4 w-32" />
				<Skeleton className="mt-5 h-3.5 w-full max-w-2xl" />
				<Skeleton className="mt-2 h-3.5 w-2/3 max-w-md" />
			</div>
			<div className={ACTIONS_CLASS}>
				<Skeleton className="h-9 w-36" />
				<Skeleton className="h-9 w-24" />
				{canDownload ? <Skeleton className="h-9 w-28" /> : null}
			</div>
		</LoadingState>
	);
}
