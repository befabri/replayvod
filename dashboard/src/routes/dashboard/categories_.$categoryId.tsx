import { createFileRoute } from "@tanstack/react-router";
import { useTranslation } from "react-i18next";
import {
	TitleBreadcrumb,
	TitleBreadcrumbParentLink,
	TitledLayout,
} from "@/components/layout/titled-layout";
import { Alert } from "@/components/ui/alert";
import { EmptyPanel } from "@/components/ui/empty-panel";
import {
	CategoryHeader,
	CategoryHeaderSkeleton,
} from "@/features/categories/components/CategoryHeader";
import { useCategoryDetail } from "@/features/categories/queries";
import { VideoGridEnd } from "@/features/videos/components/VideoGridEnd";
import { VideoGridLoading } from "@/features/videos/components/VideoGridLoading";
import { VirtualVideoGrid } from "@/features/videos/components/VirtualVideoGrid";
import { useCanManageVideos } from "@/features/videos/permissions";
import { useInfiniteVideosByCategory } from "@/features/videos/queries";
import { useInfiniteResource } from "@/hooks/useInfiniteResource";

export const Route = createFileRoute("/dashboard/categories_/$categoryId")({
	component: CategoryDetailPage,
});

function CategoryDetailPage() {
	const { t } = useTranslation();
	const { categoryId } = Route.useParams();
	const category = useCategoryDetail(categoryId);
	const videos = useInfiniteVideosByCategory(categoryId, 24);
	const resource = useInfiniteResource(videos, {
		getItems: (page) => page.items,
	});
	const videoItems = resource.items;
	const canManage = useCanManageVideos();

	return (
		<TitledLayout
			title={
				<TitleBreadcrumb
					parent={
						<TitleBreadcrumbParentLink
							to="/dashboard/categories"
							search={{ sort: "name_asc" }}
						>
							{t("nav.categories")}
						</TitleBreadcrumbParentLink>
					}
					currentLabel={category.data?.name ?? t("categories.detail_fallback")}
				/>
			}
		>
			{category.isLoading && <CategoryHeaderSkeleton />}
			{category.error && (
				<Alert variant="destructive" className="mt-4">
					{category.error.message}
				</Alert>
			)}

			{category.data && <CategoryHeader category={category.data} />}

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
						canManage={canManage}
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
