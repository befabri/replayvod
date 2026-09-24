import { useTranslation } from "react-i18next";
import type { CategoryDetailResponse } from "@/api/generated/trpc";
import { ExpandableText } from "@/components/ui/expandable-text";
import { LoadingState } from "@/components/ui/loading-state";
import { Skeleton } from "@/components/ui/skeleton";
import { formatBytes } from "@/features/videos/format";
import { cn } from "@/lib/utils";
import { CategoryBoxArt } from "./CategoryBoxArt";

const HEADER_CLASS =
	"mt-4 mb-8 flex flex-col gap-5 sm:flex-row sm:items-start sm:gap-6";
const BOX_ART_CLASS = "w-36 rounded-md shrink-0";

export function CategoryHeader({
	category,
}: {
	category: CategoryDetailResponse;
}) {
	const { t } = useTranslation();
	return (
		<div className={HEADER_CLASS}>
			<CategoryBoxArt
				url={category.box_art_url}
				name={category.name}
				width={144}
				height={192}
				sizes="144px"
				className={BOX_ART_CLASS}
			/>
			<div className="flex-1 min-w-0">
				<div className="text-muted-foreground mt-0.5">
					{t("categories.detail_summary", {
						count: category.video_count,
						size: formatCategorySize(category.total_size),
					})}
				</div>
				{category.description && (
					<ExpandableText className="mt-3 max-w-2xl text-sm leading-6">
						{category.description}
					</ExpandableText>
				)}
			</div>
		</div>
	);
}

export function CategoryHeaderSkeleton() {
	return (
		<LoadingState className={HEADER_CLASS}>
			<Skeleton className={cn("aspect-[3/4]", BOX_ART_CLASS)} />
			<div className="flex-1 min-w-0">
				<Skeleton className="mt-1.5 h-4 w-48" />
				<Skeleton className="mt-5 h-3.5 w-full max-w-2xl" />
				<Skeleton className="mt-2.5 h-3.5 w-full max-w-2xl" />
				<Skeleton className="mt-2.5 h-3.5 w-1/2 max-w-sm" />
			</div>
		</LoadingState>
	);
}

function formatCategorySize(bytes: number) {
	return bytes > 0 ? formatBytes(bytes) : "0 B";
}
