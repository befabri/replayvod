import { Link, createFileRoute } from "@tanstack/react-router";
import { useTranslation } from "react-i18next";
import { TitledLayout } from "@/components/layout/titled-layout";
import { CategoryBoxArt } from "@/features/categories/components/CategoryBoxArt";
import { useCategory } from "@/features/categories/queries";
import { VideoCard } from "@/features/videos/components/VideoCard";
import { useVideosByCategory } from "@/features/videos/queries";

export const Route = createFileRoute("/dashboard/categories_/$categoryId")({
	component: CategoryDetailPage,
});

function CategoryDetailPage() {
	const { t } = useTranslation();
	const { categoryId } = Route.useParams();
	const category = useCategory(categoryId);
	const videos = useVideosByCategory(categoryId, 50, 0);

	return (
		<TitledLayout title={category.data?.name ?? ""}>
			<Link
				// biome-ignore lint/suspicious/noExplicitAny: static route typing
				to={"/dashboard/categories" as any}
				className="text-sm text-muted-foreground hover:text-foreground -mt-6 mb-4 inline-block"
			>
				← {t("nav.categories")}
			</Link>

			{category.isLoading && (
				<div className="text-muted-foreground mt-4">{t("common.loading")}</div>
			)}
			{category.error && (
				<div className="mt-4 rounded-lg bg-destructive/10 p-4 text-destructive text-sm shadow-sm">
					{category.error.message}
				</div>
			)}

			{category.data && (
				<div className="mt-4 flex gap-6 items-start mb-8">
					<CategoryBoxArt
						url={category.data.box_art_url}
						name={category.data.name}
						width={192}
						height={256}
						className="w-36 rounded-md shrink-0"
					/>
					<div className="flex-1 min-w-0">
						<div className="font-mono text-xs text-muted-foreground">
							{category.data.id}
						</div>
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
