import { createFileRoute, Link } from "@tanstack/react-router";
import { useEffect, useRef } from "react";
import { useTranslation } from "react-i18next";
import { TitledLayout } from "@/components/layout/titled-layout";
import { CategoryBoxArt } from "@/features/categories/components/CategoryBoxArt";
import { useCategory } from "@/features/categories/queries";
import { VideoGridEnd } from "@/features/videos/components/VideoGridEnd";
import { VideoGridLoading } from "@/features/videos/components/VideoGridLoading";
import { VirtualVideoGrid } from "@/features/videos/components/VirtualVideoGrid";
import { useInfiniteVideosByCategory } from "@/features/videos/queries";

export const Route = createFileRoute("/dashboard/categories_/$categoryId")({
	component: CategoryDetailPage,
});

function CategoryDetailPage() {
	const { t } = useTranslation();
	const { categoryId } = Route.useParams();
	const category = useCategory(categoryId);
	const videos = useInfiniteVideosByCategory(categoryId, 24);
	const loadMoreRef = useRef<HTMLDivElement | null>(null);
	const videoItems = videos.data?.pages.flatMap((page) => page.items) ?? [];
	const hasScrolledThroughPages = (videos.data?.pages.length ?? 0) > 1;

	useEffect(() => {
		const node = loadMoreRef.current;
		if (!node || !videos.hasNextPage) {
			return;
		}
		const observer = new IntersectionObserver(
			(entries) => {
				if (!entries[0]?.isIntersecting || videos.isFetchingNextPage) {
					return;
				}
				void videos.fetchNextPage();
			},
			{ rootMargin: "400px 0px" },
		);
		observer.observe(node);
		return () => observer.disconnect();
	}, [videos.fetchNextPage, videos.hasNextPage, videos.isFetchingNextPage]);

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

			{videos.isLoading && <VideoGridLoading className="mt-0" variant="wide" />}
			{videos.data && videoItems.length === 0 && (
				<div className="text-muted-foreground">{t("videos.empty")}</div>
			)}
			{videos.data && videoItems.length > 0 && (
				<>
					<VirtualVideoGrid videos={videoItems} variant="wide" />
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
