import { createFileRoute } from "@tanstack/react-router";
import { useSelector } from "@tanstack/react-store";
import { useTranslation } from "react-i18next";
import {
	TitleBreadcrumb,
	TitleBreadcrumbParentLink,
	TitledLayout,
} from "@/components/layout/titled-layout";
import { Alert } from "@/components/ui/alert";
import { EmptyPanel } from "@/components/ui/empty-panel";
import {
	ChannelHeader,
	ChannelHeaderSkeleton,
} from "@/features/channels/components/ChannelHeader";
import { useChannel } from "@/features/channels/queries";
import { useLiveSet } from "@/features/streams-live";
import { VideoGridEnd } from "@/features/videos/components/VideoGridEnd";
import { VideoGridLoading } from "@/features/videos/components/VideoGridLoading";
import { VirtualVideoGrid } from "@/features/videos/components/VirtualVideoGrid";
import { useInfiniteVideosByBroadcaster } from "@/features/videos/queries";
import { useInfiniteResource } from "@/hooks/useInfiniteResource";
import { authStore, hasRole } from "@/stores/auth";

export const Route = createFileRoute("/dashboard/channels_/$channelId")({
	component: ChannelDetailPage,
});

function ChannelDetailPage() {
	const { t } = useTranslation();
	const { channelId } = Route.useParams();
	const channel = useChannel(channelId);
	const videos = useInfiniteVideosByBroadcaster(channelId, 24);
	const resource = useInfiniteResource(videos, {
		getItems: (page) => page.items,
	});
	const liveSet = useLiveSet();
	const isLive = liveSet.has(channelId);
	const user = useSelector(authStore, (s) => s.user);
	const canDownload = hasRole(user, "admin");
	const videoItems = resource.items;

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
			{channel.isLoading && <ChannelHeaderSkeleton canDownload={canDownload} />}
			{channel.error && (
				<Alert variant="destructive" className="mt-4">
					{channel.error.message}
				</Alert>
			)}

			{channel.data && (
				<ChannelHeader
					channel={channel.data}
					isLive={isLive}
					canDownload={canDownload}
				/>
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
					<div ref={resource.loadMoreRef} className="h-1" />
					{videos.isFetchingNextPage && (
						<VideoGridLoading count={2} variant="wide" />
					)}
					{resource.showEnd && <VideoGridEnd />}
				</>
			)}
		</TitledLayout>
	);
}
