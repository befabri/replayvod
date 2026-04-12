import { Link, createFileRoute } from "@tanstack/react-router"
import { useTranslation } from "react-i18next"
import { useCategory } from "@/features/categories/queries"
import { VideoCard } from "@/features/videos/components/VideoCard"
import { useVideosByCategory } from "@/features/videos/queries"

export const Route = createFileRoute("/dashboard/categories_/$categoryId")({
	component: CategoryDetailPage,
})

function CategoryDetailPage() {
	const { t } = useTranslation()
	const { categoryId } = Route.useParams()
	const category = useCategory(categoryId)
	const videos = useVideosByCategory(categoryId, 50, 0)

	return (
		<div className="p-8">
			<Link
				// biome-ignore lint/suspicious/noExplicitAny: static route typing
				to={"/dashboard/categories" as any}
				className="text-sm text-muted-foreground hover:text-foreground"
			>
				← {t("nav.categories")}
			</Link>

			{category.isLoading && (
				<div className="text-muted-foreground mt-4">Loading…</div>
			)}
			{category.error && (
				<div className="mt-4 rounded-md bg-destructive/10 border border-destructive/20 p-4 text-destructive text-sm">
					{category.error.message}
				</div>
			)}

			{category.data && (
				<div className="mt-4 flex gap-6 items-start mb-8">
					{category.data.box_art_url && (
						<img
							src={category.data.box_art_url
								.replace("{width}", "144")
								.replace("{height}", "192")}
							alt=""
							className="w-36 aspect-[3/4] rounded-md object-cover flex-shrink-0"
						/>
					)}
					<h1 className="text-3xl font-heading font-bold flex-1 min-w-0">
						{category.data.name}
					</h1>
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
				<div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 gap-4">
					{videos.data.map((v) => (
						<VideoCard key={v.id} video={v} />
					))}
				</div>
			)}
		</div>
	)
}
