import { DownloadIcon, TwitchLogoIcon } from "@phosphor-icons/react";
import { createFileRoute } from "@tanstack/react-router";
import { useSelector } from "@tanstack/react-store";
import { useTranslation } from "react-i18next";
import {
	TitleBreadcrumb,
	TitleBreadcrumbParentLink,
	TitledLayout,
} from "@/components/layout/titled-layout";
import { Avatar } from "@/components/ui/avatar";
import { Button, buttonVariants } from "@/components/ui/button";
import { EmptyPanel } from "@/components/ui/empty-panel";
import { ChannelFavoriteButton } from "@/features/channels/components/ChannelFavoriteButton";
import { useChannel } from "@/features/channels/queries";
import { useLiveSet } from "@/features/streams-live";
import { ChannelDownloadDialog } from "@/features/videos/components/ChannelDownloadDialog";
import { VideoGridEnd } from "@/features/videos/components/VideoGridEnd";
import { VideoGridLoading } from "@/features/videos/components/VideoGridLoading";
import { VirtualVideoGrid } from "@/features/videos/components/VirtualVideoGrid";
import { useInfiniteVideosByBroadcaster } from "@/features/videos/queries";
import { useInfiniteScrollSentinel } from "@/hooks/useInfiniteScrollSentinel";
import { authStore, hasRole } from "@/stores/auth";

export const Route = createFileRoute("/dashboard/channels_/$channelId")({
	component: ChannelDetailPage,
});

function ChannelDetailPage() {
	const { t } = useTranslation();
	const { channelId } = Route.useParams();
	const channel = useChannel(channelId);
	const videos = useInfiniteVideosByBroadcaster(channelId, 24);
	const liveSet = useLiveSet();
	const isLive = liveSet.has(channelId);
	// Both the direct download (video.triggerDownload) and the schedule tab
	// (schedule.create) are admin-only on the server, so the whole Download
	// entry point is hidden from viewers rather than letting them fill the
	// flow and fail on submit.
	const user = useSelector(authStore, (s) => s.user);
	const canDownload = hasRole(user, "admin");
	const videoItems = videos.data?.pages.flatMap((page) => page.items) ?? [];
	const hasScrolledThroughPages = (videos.data?.pages.length ?? 0) > 1;
	const loadMoreRef = useInfiniteScrollSentinel({
		enabled: !!videos.hasNextPage,
		isLoadingMore: videos.isFetchingNextPage,
		onLoadMore: () => videos.fetchNextPage(),
	});

	return (
		<TitledLayout
			title={
				<TitleBreadcrumb
					parent={
						<TitleBreadcrumbParentLink
							to="/dashboard/channels"
							search={{ sort: "name_asc", filter: "all" }}
						>
							{t("nav.channels")}
						</TitleBreadcrumbParentLink>
					}
					currentLabel={channel.data?.broadcaster_name ?? channelId}
				/>
			}
		>
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
						<ChannelFavoriteButton
							broadcasterId={channel.data.broadcaster_id}
							favorite={channel.data.user_state?.favorite ?? false}
							withLabel
						/>
						<a
							href={`https://twitch.tv/${channel.data.broadcaster_login}`}
							target="_blank"
							rel="noopener noreferrer"
							className={buttonVariants({ variant: "outline" })}
						>
							<TwitchLogoIcon weight="fill" />
							{t("channels.open_in_twitch")}
						</a>
						{canDownload && (
							<ChannelDownloadDialog
								broadcasterId={channel.data.broadcaster_id}
								broadcasterName={channel.data.broadcaster_name}
								broadcasterLogin={channel.data.broadcaster_login}
								profileImageUrl={channel.data.profile_image_url}
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
			)}

			<h2 className="text-xl font-medium mb-4">{t("nav.videos")}</h2>

			{videos.isLoading && <VideoGridLoading className="mt-0" variant="wide" />}
			{videos.data && videoItems.length === 0 && (
				<EmptyPanel>{t("videos.empty")}</EmptyPanel>
			)}
			{videos.data && videoItems.length > 0 && (
				<>
					<VirtualVideoGrid
						videos={videoItems}
						variant="wide"
						canManage={canDownload}
					/>
					<div ref={loadMoreRef} className="h-1" />
					{videos.isFetchingNextPage && (
						<VideoGridLoading count={2} variant="wide" />
					)}
					{hasScrolledThroughPages &&
						!videos.hasNextPage &&
						!videos.isFetchingNextPage && <VideoGridEnd />}
				</>
			)}
		</TitledLayout>
	);
}
