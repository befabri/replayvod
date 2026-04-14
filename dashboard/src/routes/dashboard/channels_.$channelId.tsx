import { Download, TwitchLogo } from "@phosphor-icons/react";
import { Link, createFileRoute } from "@tanstack/react-router";
import { useTranslation } from "react-i18next";
import { TitledLayout } from "@/components/layout/titled-layout";
import { Avatar } from "@/components/ui/avatar";
import { Button, buttonVariants } from "@/components/ui/button";
import { useChannel } from "@/features/channels/queries";
import { useLiveSet } from "@/features/streams-live";
import { TriggerDownloadDialog } from "@/features/videos/components/TriggerDownloadDialog";
import { VideoCard } from "@/features/videos/components/VideoCard";
import { useVideosByBroadcaster } from "@/features/videos/queries";

export const Route = createFileRoute("/dashboard/channels_/$channelId")({
	component: ChannelDetailPage,
});

function ChannelDetailPage() {
	const { t } = useTranslation();
	const { channelId } = Route.useParams();
	const channel = useChannel(channelId);
	const videos = useVideosByBroadcaster(channelId, 50, 0);
	const liveSet = useLiveSet();
	const isLive = liveSet.has(channelId);

	return (
		<TitledLayout title={channel.data?.broadcaster_name ?? ""}>
			<Link
				// biome-ignore lint/suspicious/noExplicitAny: static route typing
				to={"/dashboard/channels" as any}
				className="text-sm text-muted-foreground hover:text-foreground -mt-6 mb-4 inline-block"
			>
				← {t("nav.channels")}
			</Link>

			{channel.isLoading && (
				<div className="text-muted-foreground mt-4">{t("common.loading")}</div>
			)}
			{channel.error && (
				<div className="mt-4 rounded-lg bg-destructive/10 p-4 text-destructive text-sm shadow-sm">
					{channel.error.message}
				</div>
			)}

			{channel.data && (
				<div className="mt-4 flex gap-6 items-start mb-8">
					<Avatar
						src={channel.data.profile_image_url}
						name={channel.data.broadcaster_name}
						alt={channel.data.broadcaster_name}
						size="3xl"
						isLive={isLive}
						liveRingClass="ring-background"
					/>
					<div className="flex-1 min-w-0">
						<div className="text-muted-foreground mt-0.5">
							@{channel.data.broadcaster_login}
						</div>
						{channel.data.description && (
							<p className="text-sm mt-3 max-w-2xl">
								{channel.data.description}
							</p>
						)}
					</div>
					<div className="flex items-center gap-2 shrink-0">
						<a
							href={`https://twitch.tv/${channel.data.broadcaster_login}`}
							target="_blank"
							rel="noopener noreferrer"
							className={buttonVariants({ variant: "outline" })}
						>
							<TwitchLogo weight="fill" />
							{t("channels.open_in_twitch")}
						</a>
						<TriggerDownloadDialog
							broadcasterId={channel.data.broadcaster_id}
							broadcasterName={channel.data.broadcaster_name}
						>
							<Button variant="outline">
								<Download weight="regular" />
								{t("videos.trigger_download")}
							</Button>
						</TriggerDownloadDialog>
					</div>
				</div>
			)}

			<h2 className="text-xl font-medium mb-4">{t("nav.videos")}</h2>

			{videos.isLoading && (
				<div className="text-muted-foreground">{t("common.loading")}</div>
			)}
			{videos.data && videos.data.length === 0 && (
				<div className="text-muted-foreground">{t("videos.empty")}</div>
			)}
			{videos.data && videos.data.length > 0 && (
				<div className="grid grid-cols-[repeat(auto-fit,minmax(400px,1fr))] gap-4">
					{videos.data.map((v) => (
						<VideoCard key={v.id} video={v} />
					))}
				</div>
			)}
		</TitledLayout>
	);
}
