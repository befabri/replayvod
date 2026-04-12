import { createFileRoute } from "@tanstack/react-router"
import { useTranslation } from "react-i18next"
import { useChannels } from "@/features/channels"

export const Route = createFileRoute("/dashboard/channels")({
	component: ChannelsPage,
})

function ChannelsPage() {
	const { t } = useTranslation()
	const { data: channels, isLoading, error } = useChannels()

	return (
		<div className="p-8">
			<h1 className="text-3xl font-heading font-bold mb-6">
				{t("nav.channels")}
			</h1>

			{isLoading && (
				<div className="text-muted-foreground">Loading channels…</div>
			)}

			{error && (
				<div className="rounded-md bg-destructive/10 border border-destructive/20 p-4 text-destructive text-sm">
					Failed to load channels: {error.message}
				</div>
			)}

			{channels && channels.length === 0 && (
				<div className="text-muted-foreground">
					No channels yet. Channels will appear here as you follow broadcasters
					or configure download schedules.
				</div>
			)}

			{channels && channels.length > 0 && (
				<div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-4">
					{channels.map((c) => (
						<div
							key={c.broadcaster_id}
							className="rounded-lg border border-border bg-card p-4 flex gap-4 items-start"
						>
							{c.profile_image_url && (
								<img
									src={c.profile_image_url}
									alt=""
									className="w-16 h-16 rounded-full flex-shrink-0"
								/>
							)}
							<div className="flex-1 min-w-0">
								<div className="font-semibold truncate">
									{c.broadcaster_name}
								</div>
								<div className="text-sm text-muted-foreground truncate">
									@{c.broadcaster_login}
								</div>
								{c.description && (
									<div className="text-sm text-muted-foreground mt-2 line-clamp-2">
										{c.description}
									</div>
								)}
							</div>
						</div>
					))}
				</div>
			)}
		</div>
	)
}
