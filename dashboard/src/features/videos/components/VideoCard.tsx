import { Link } from "@tanstack/react-router"
import { useTranslation } from "react-i18next"
import { API_URL } from "@/env"
import type { VideoResponse } from "@/features/videos"
import { formatBytes, formatDuration } from "@/features/videos/format"

function InlineStatusBadge({ status }: { status: string }) {
	const { t } = useTranslation()
	const color =
		status === "DONE"
			? "bg-emerald-500/90"
			: status === "FAILED"
				? "bg-destructive/90"
				: status === "RUNNING"
					? "bg-primary/90"
					: "bg-muted-foreground/80"
	return (
		<span
			className={`absolute top-2 right-2 px-2 py-0.5 rounded text-xs font-medium text-white ${color}`}
		>
			{t(`videos.status.${status}` as const, status)}
		</span>
	)
}

export function VideoCard({ video }: { video: VideoResponse }) {
	const { t } = useTranslation()
	const thumbnail = video.thumbnail
		? `${API_URL}/api/v1/thumbnails/${video.thumbnail.replace(/^thumbnails\//, "")}`
		: null
	return (
		<div className="rounded-lg border border-border bg-card overflow-hidden flex flex-col">
			<div className="aspect-video bg-muted flex items-center justify-center relative">
				{thumbnail ? (
					<img
						src={thumbnail}
						alt=""
						className="w-full h-full object-cover"
						loading="lazy"
					/>
				) : (
					<div className="text-muted-foreground text-sm">No thumbnail</div>
				)}
				<InlineStatusBadge status={video.status} />
			</div>
			<div className="p-3 flex-1 flex flex-col">
				<div className="font-medium truncate" title={video.display_name}>
					{video.display_name}
				</div>
				<div className="text-sm text-muted-foreground mt-1 flex gap-3">
					<span>{formatDuration(video.duration_seconds)}</span>
					<span>{formatBytes(video.size_bytes)}</span>
				</div>
				<div className="mt-auto pt-3">
					{video.status === "DONE" ? (
						<Link
							// biome-ignore lint/suspicious/noExplicitAny: param route typing
							to={"/dashboard/watch/$videoId" as any}
							params={{ videoId: String(video.id) } as any}
							className="text-sm text-primary hover:underline"
						>
							{t("videos.watch")} →
						</Link>
					) : (
						<span className="text-sm text-muted-foreground">
							{t(`videos.status.${video.status}` as const)}
						</span>
					)}
				</div>
			</div>
		</div>
	)
}
