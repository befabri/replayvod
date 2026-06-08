import { SortAscendingIcon } from "@phosphor-icons/react";
import { createFileRoute, Link } from "@tanstack/react-router";
import { useCallback } from "react";
import { useTranslation } from "react-i18next";
import { TitledLayout } from "@/components/layout/titled-layout";
import { Button } from "@/components/ui/button";
import {
	DropdownMenu,
	DropdownMenuContent,
	DropdownMenuItem,
	DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { EmptyPanel } from "@/components/ui/empty-panel";
import { VirtualGrid } from "@/components/ui/virtual-grid";
import {
	type CategoryResponse,
	type CategorySort,
	useInfiniteCategoriesWithVideos,
} from "@/features/categories";
import { CategoryBoxArt } from "@/features/categories/components/CategoryBoxArt";
import { VideoGridEnd } from "@/features/videos/components/VideoGridEnd";
import { useInfiniteResource } from "@/hooks/useInfiniteResource";

const SORT_MODES = [
	"name_asc",
	"latest_video_desc",
	"video_count_desc",
] as const satisfies readonly CategorySort[];
type SortMode = (typeof SORT_MODES)[number];

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
				<div className="text-muted-foreground">{t("common.loading")}</div>
			)}

			{categories.error && (
				<div className="rounded-lg bg-destructive/10 p-4 text-destructive text-sm shadow-sm">
					{t("categories.failed_to_load")}: {categories.error.message}
				</div>
			)}

			{visible.length === 0 &&
				!categories.isLoading &&
				!categories.isFetchingNextPage &&
				!categories.error && <EmptyPanel>{t("categories.empty")}</EmptyPanel>}

			{visible.length > 0 && <CategoryGrid categories={visible} />}
			{resource.shouldLoadMore && (
				<div ref={resource.loadMoreRef} className="h-1" />
			)}
			{categories.isFetchingNextPage && (
				<div className="mt-4 text-muted-foreground text-sm">
					{t("common.loading")}
				</div>
			)}
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
				className="group flex flex-col gap-1.5"
			>
				<CategoryBoxArt
					url={category.box_art_url}
					name={category.name}
					width={180}
					height={240}
					sizes="(max-width: 768px) calc(50vw - 1.5rem), 180px"
					className="rounded-md border-4 border-background group-hover:border-primary transition-colors duration-75"
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
			minItemWidth={140}
			estimateRowHeight={230}
			gap={12}
			overscan={6}
		/>
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
