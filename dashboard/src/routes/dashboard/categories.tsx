import { SortAscendingIcon } from "@phosphor-icons/react";
import { createFileRoute, Link } from "@tanstack/react-router";
import { useCallback } from "react";
import { useTranslation } from "react-i18next";
import { TitledLayout } from "@/components/layout/titled-layout";
import { Alert } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import {
	DropdownMenu,
	DropdownMenuContent,
	DropdownMenuItem,
	DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { EmptyPanel } from "@/components/ui/empty-panel";
import { LoadingState } from "@/components/ui/loading-state";
import { Skeleton } from "@/components/ui/skeleton";
import { VirtualGrid, VirtualGridSkeleton } from "@/components/ui/virtual-grid";
import {
	type CategoryResponse,
	type CategorySort,
	useInfiniteCategoriesWithVideos,
} from "@/features/categories";
import { CategoryBoxArt } from "@/features/categories/components/CategoryBoxArt";
import { VideoGridEnd } from "@/features/videos/components/VideoGridEnd";
import { useInfiniteResource } from "@/hooks/useInfiniteResource";
import { cn } from "@/lib/utils";

const SORT_MODES = [
	"name_asc",
	"latest_video_desc",
	"video_count_desc",
] as const satisfies readonly CategorySort[];
type SortMode = (typeof SORT_MODES)[number];

const CATEGORY_GRID = { minItemWidth: 140, gap: 12 } as const;
const CATEGORY_TILE_CLASS = "group flex flex-col gap-1.5";
const CATEGORY_ART_CLASS = "rounded-md border-4 border-background";
const LOADING_TILES = 12;

export const Route = createFileRoute("/dashboard/categories")({
	validateSearch: (search: Record<string, unknown>) => ({
		sort: SORT_MODES.includes(search.sort as SortMode)
			? (search.sort as SortMode)
			: ("name_asc" as SortMode),
	}),
	component: CategoriesPage,
});

function CategoriesPage() {
	const { t } = useTranslation();
	const { sort } = Route.useSearch();
	const navigate = Route.useNavigate();
	const categories = useInfiniteCategoriesWithVideos(sort);
	const resource = useInfiniteResource(categories, {
		getItems: (page) => page.items,
		rootMargin: "500px 0px",
	});
	const visible = resource.items;

	return (
		<TitledLayout
			title={t("categories.title")}
			actions={
				<SortDropdown
					current={sort}
					onChange={(next) =>
						void navigate({ search: (s) => ({ ...s, sort: next }) })
					}
				/>
			}
		>
			{categories.isLoading && (
				<VirtualGridSkeleton
					count={LOADING_TILES}
					renderItem={() => <CategoryTileSkeleton />}
					{...CATEGORY_GRID}
				/>
			)}

			{categories.error && (
				<Alert variant="destructive">
					{t("categories.failed_to_load")}: {categories.error.message}
				</Alert>
			)}

			{visible.length === 0 &&
				!categories.isLoading &&
				!categories.isFetchingNextPage &&
				!categories.error && <EmptyPanel>{t("categories.empty")}</EmptyPanel>}

			{visible.length > 0 && <CategoryGrid categories={visible} />}
			{resource.shouldLoadMore && (
				<div ref={resource.loadMoreRef} className="h-1" />
			)}
			{categories.isFetchingNextPage && <LoadingState className="mt-4" />}
			{resource.showEnd && <VideoGridEnd labelKey="categories.end_of_list" />}
		</TitledLayout>
	);
}

function CategoryGrid({ categories }: { categories: CategoryResponse[] }) {
	const getCategoryKey = useCallback(
		(category: CategoryResponse) => category.id,
		[],
	);
	const renderCategory = useCallback(
		(category: CategoryResponse) => (
			<Link
				to="/dashboard/categories/$categoryId"
				params={{ categoryId: category.id }}
				className={CATEGORY_TILE_CLASS}
			>
				<CategoryBoxArt
					url={category.box_art_url}
					name={category.name}
					decorative
					width={180}
					height={240}
					sizes="(max-width: 768px) calc(50vw - 1.5rem), 180px"
					className={cn(
						CATEGORY_ART_CLASS,
						"group-hover:border-primary transition-colors duration-75",
					)}
				/>
				<div className="text-sm font-medium truncate group-hover:text-link transition-colors duration-75">
					{category.name}
				</div>
			</Link>
		),
		[],
	);

	return (
		<VirtualGrid
			items={categories}
			getItemKey={getCategoryKey}
			renderItem={renderCategory}
			estimateRowHeight={230}
			overscan={6}
			{...CATEGORY_GRID}
		/>
	);
}

function CategoryTileSkeleton() {
	return (
		<div className={CATEGORY_TILE_CLASS}>
			<Skeleton className={cn("aspect-[3/4] w-full", CATEGORY_ART_CLASS)} />
			<Skeleton className="my-0.5 h-4 w-3/4" />
		</div>
	);
}

function SortDropdown({
	current,
	onChange,
}: {
	current: SortMode;
	onChange: (mode: SortMode) => void;
}) {
	const { t } = useTranslation();
	const labels: Record<SortMode, string> = {
		name_asc: t("categories.sort_default"),
		latest_video_desc: t("categories.sort_latest_video"),
		video_count_desc: t("categories.sort_video_count"),
	};
	return (
		<DropdownMenu>
			<DropdownMenuTrigger
				render={(triggerProps) => (
					<Button variant="outline" size="sm" {...triggerProps}>
						<SortAscendingIcon className="size-4" />
						{labels[current]}
					</Button>
				)}
			/>
			<DropdownMenuContent>
				{SORT_MODES.map((mode) => (
					<DropdownMenuItem key={mode} onClick={() => onChange(mode)}>
						{labels[mode]}
					</DropdownMenuItem>
				))}
			</DropdownMenuContent>
		</DropdownMenu>
	);
}
