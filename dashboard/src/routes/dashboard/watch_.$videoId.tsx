import { Link, createFileRoute } from "@tanstack/react-router"
import { API_URL } from "@/env"
import { useVideo } from "@/features/videos"
import { formatBytes, formatDuration } from "@/features/videos/format"

export const Route = createFileRoute("/dashboard/watch_/$videoId")({
	component: WatchPage,
})

function WatchPage() {
	const { videoId } = Route.useParams()
	const id = Number(videoId)
	const { data: video, isLoading, error } = useVideo(id)

	if (isLoading) {
		return <div className="p-8 text-muted-foreground">Loading…</div>
	}
	if (error) {
		return (
			<div className="p-8">
				<div className="rounded-md bg-destructive/10 border border-destructive/20 p-4 text-destructive text-sm">
					{error.message}
				</div>
			</div>
		)
	}
	if (!video) {
		return <div className="p-8 text-muted-foreground">Video not found.</div>
	}

	if (video.status !== "DONE") {
		return (
			<div className="p-8 max-w-3xl">
				<h1 className="text-2xl font-heading font-bold mb-2">
					{video.display_name}
				</h1>
				<p className="text-muted-foreground">
					This video is not ready to play (status: {video.status}).
				</p>
				<Link
					// biome-ignore lint/suspicious/noExplicitAny: static route typing
					to={"/dashboard/videos" as any}
					className="inline-block mt-4 text-primary hover:underline"
				>
					← Back to videos
				</Link>
			</div>
		)
	}

	// Credentials:"include" is set globally on the tRPC client but the
	// <video> element has no such override, so the browser uses its default
	// (same-origin only). When API_URL is cross-origin, CORS+Credentials on
	// the streaming endpoint must be configured — handled in CORS middleware.
	const streamURL = `${API_URL}/api/v1/videos/${video.id}/stream`

	return (
		<div className="p-4 md:p-8 max-w-5xl mx-auto">
			<h1 className="text-2xl font-heading font-bold mb-4">
				{video.display_name}
			</h1>

			<div className="rounded-lg overflow-hidden border border-border bg-black">
				{/* biome-ignore lint/a11y/useMediaCaption: no captions available from Twitch VODs */}
				<video
					controls
					preload="metadata"
					className="w-full aspect-video"
					src={streamURL}
				/>
			</div>

			<div className="mt-4 grid grid-cols-2 md:grid-cols-4 gap-4 text-sm">
				<Metric label="Duration" value={formatDuration(video.duration_seconds)} />
				<Metric label="Size" value={formatBytes(video.size_bytes)} />
				<Metric label="Quality" value={video.quality} />
				<Metric label="Language" value={video.language || "—"} />
			</div>
		</div>
	)
}

function Metric({ label, value }: { label: string; value: string }) {
	return (
		<div className="rounded-md border border-border bg-card p-3">
			<div className="text-xs text-muted-foreground">{label}</div>
			<div className="text-base font-medium mt-0.5">{value}</div>
		</div>
	)
}
