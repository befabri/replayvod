import { Link, createFileRoute } from "@tanstack/react-router"
import { useState } from "react"
import { useTranslation } from "react-i18next"
import { useVideos } from "@/features/videos"
import { VideoCard } from "@/features/videos/components/VideoCard"

const PAGE_SIZE = 50

export const Route = createFileRoute("/dashboard/videos")({
	validateSearch: (search: Record<string, unknown>) => ({
		status: typeof search.status === "string" ? search.status : undefined,
	}),
	component: VideosPage,
})

function VideosPage() {
	const { t } = useTranslation()
	const { status } = Route.useSearch()
	const [page, setPage] = useState(0)
	const { data, isLoading, error } = useVideos(PAGE_SIZE, page * PAGE_SIZE, status)

	return (
		<div className="p-8">
			<div className="flex items-center justify-between mb-6">
				<h1 className="text-3xl font-heading font-bold">{t("videos.title")}</h1>
				<StatusFilter current={status ?? "all"} />
			</div>

			{isLoading && <div className="text-muted-foreground">Loading…</div>}

			{error && (
				<div className="rounded-md bg-destructive/10 border border-destructive/20 p-4 text-destructive text-sm">
					{t("videos.failed_to_load")}: {error.message}
				</div>
			)}

			{data && data.length === 0 && !isLoading && !error && (
				<div className="text-muted-foreground">{t("videos.empty")}</div>
			)}

			{data && data.length > 0 && (
				<>
					<div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 gap-4">
						{data.map((v) => (
							<VideoCard key={v.id} video={v} />
						))}
					</div>
					<div className="flex items-center gap-2 mt-6">
						<button
							type="button"
							disabled={page === 0}
							onClick={() => setPage((p) => Math.max(0, p - 1))}
							className="px-3 py-1 rounded-md border border-border disabled:opacity-50"
						>
							Previous
						</button>
						<span className="text-sm text-muted-foreground">Page {page + 1}</span>
						<button
							type="button"
							disabled={data.length < PAGE_SIZE}
							onClick={() => setPage((p) => p + 1)}
							className="px-3 py-1 rounded-md border border-border disabled:opacity-50"
						>
							Next
						</button>
					</div>
				</>
			)}
		</div>
	)
}

function StatusFilter({ current }: { current: string }) {
	const options: Array<{ value: string; label: string }> = [
		{ value: "all", label: "All" },
		{ value: "DONE", label: "Done" },
		{ value: "RUNNING", label: "Running" },
		{ value: "PENDING", label: "Pending" },
		{ value: "FAILED", label: "Failed" },
	]
	return (
		<div className="flex gap-1">
			{options.map((o) => (
				<Link
					key={o.value}
					// biome-ignore lint/suspicious/noExplicitAny: static route typing
					to={"/dashboard/videos" as any}
					search={
						(o.value === "all"
							? { status: undefined }
							: { status: o.value }) as any
					}
					className={`px-3 py-1 rounded-md text-sm border transition-colors ${
						current === o.value
							? "bg-primary text-primary-foreground border-primary"
							: "border-border hover:bg-muted"
					}`}
				>
					{o.label}
				</Link>
			))}
		</div>
	)
}
