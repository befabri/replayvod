import { createFileRoute } from "@tanstack/react-router";
import { useTranslation } from "react-i18next";
import {
	TitleBreadcrumb,
	TitleBreadcrumbParentLink,
	TitledLayout,
} from "@/components/layout/titled-layout";
import { EmptyPanel } from "@/components/ui/empty-panel";
import { ExpandableText } from "@/components/ui/expandable-text";
import { CategoryBoxArt } from "@/features/categories/components/CategoryBoxArt";
import { useCategoryDetail } from "@/features/categories/queries";
import { VideoGridEnd } from "@/features/videos/components/VideoGridEnd";
import { VideoGridLoading } from "@/features/videos/components/VideoGridLoading";
import { VirtualVideoGrid } from "@/features/videos/components/VirtualVideoGrid";
import { formatBytes } from "@/features/videos/format";
import { useCanManageVideos } from "@/features/videos/permissions";
import { useInfiniteVideosByCategory } from "@/features/videos/queries";
import { useInfiniteScrollSentinel } from "@/hooks/useInfiniteScrollSentinel";

export const Route = createFileRoute("/dashboard/categories_/$categoryId")({
	component: CategoryDetailPage,
});

function CategoryDetailPage() {
	const { t } = useTranslation();
	const { categoryId } = Route.useParams();
	const category = useCategoryDetail(categoryId);
	const videos = useInfiniteVideosByCategory(categoryId, 24);
	const videoItems = videos.data?.pages.flatMap((page) => page.items) ?? [];
	const hasScrolledThroughPages = (videos.data?.pages.length ?? 0) > 1;
	const canManage = useCanManageVideos();
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
			{category.isLoading && (
				<div className="text-muted-foreground mt-4">{t("common.loading")}</div>
			)}
			{category.error && (
				<div className="mt-4 rounded-lg bg-destructive/10 p-4 text-destructive text-sm shadow-sm">
					{category.error.message}
				</div>
			)}

			{category.data && (
				<div className="mt-4 mb-8 flex flex-col gap-5 sm:flex-row sm:items-start sm:gap-6">
					<CategoryBoxArt
						url={category.data.box_art_url}
						name={category.data.name}
						width={144}
						height={192}
						sizes="144px"
						className="w-36 rounded-md shrink-0"
					/>
					<div className="flex-1 min-w-0">
						<div className="text-muted-foreground mt-0.5">
							{t("categories.detail_summary", {
								count: category.data.video_count,
								size: formatCategorySize(category.data.total_size),
							})}
						</div>
						{category.data.description && (
							<ExpandableText className="mt-3 max-w-2xl text-sm leading-6">
								{category.data.description}
							</ExpandableText>
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
						canManage={canManage}
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

function formatCategorySize(bytes: number) {
	return bytes > 0 ? formatBytes(bytes) : "0 B";
}
