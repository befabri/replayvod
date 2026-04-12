import { Link, createFileRoute } from "@tanstack/react-router"
import { Download } from "@phosphor-icons/react"
import { useTranslation } from "react-i18next"
import { Button } from "@/components/ui/button"
import { useChannel } from "@/features/channels/queries"
import { VideoCard } from "@/features/videos/components/VideoCard"
import { TriggerDownloadDialog } from "@/features/videos/components/TriggerDownloadDialog"
import { useVideosByBroadcaster } from "@/features/videos/queries"

export const Route = createFileRoute("/dashboard/channels_/$channelId")({
	component: ChannelDetailPage,
})

function ChannelDetailPage() {
	const { t } = useTranslation()
	const { channelId } = Route.useParams()
	const channel = useChannel(channelId)
	const videos = useVideosByBroadcaster(channelId, 50, 0)

	return (
		<div className="p-8">
			<Link
				// biome-ignore lint/suspicious/noExplicitAny: static route typing
				to={"/dashboard/channels" as any}
				className="text-sm text-muted-foreground hover:text-foreground"
			>
				← {t("nav.channels")}
			</Link>

			{channel.isLoading && (
				<div className="text-muted-foreground mt-4">Loading…</div>
			)}
			{channel.error && (
				<div className="mt-4 rounded-md bg-destructive/10 border border-destructive/20 p-4 text-destructive text-sm">
					{channel.error.message}
				</div>
			)}

			{channel.data && (
				<div className="mt-4 flex gap-6 items-start mb-8">
					{channel.data.profile_image_url && (
						<img
							src={channel.data.profile_image_url}
							alt=""
							className="w-24 h-24 rounded-full flex-shrink-0"
						/>
					)}
					<div className="flex-1 min-w-0">
						<h1 className="text-3xl font-heading font-bold">
							{channel.data.broadcaster_name}
						</h1>
						<div className="text-muted-foreground mt-0.5">
							@{channel.data.broadcaster_login}
						</div>
						{channel.data.description && (
							<p className="text-sm mt-3 max-w-2xl">
								{channel.data.description}
							</p>
						)}
					</div>
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
			)}

			<h2 className="text-xl font-medium mb-4">{t("nav.videos")}</h2>

			{videos.isLoading && (
				<div className="text-muted-foreground">Loading…</div>
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
		</div>
	)
}
