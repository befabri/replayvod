import { Rows, SquaresFour } from "@phosphor-icons/react";
import { createFileRoute, Link } from "@tanstack/react-router";
import { useTranslation } from "react-i18next";
import { TitledLayout } from "@/components/layout/titled-layout";
import { Button } from "@/components/ui/button";
import {
	DropdownMenu,
	DropdownMenuContent,
	DropdownMenuItem,
	DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import type { CategoryResponse } from "@/features/categories";
import { useCategories } from "@/features/categories";
import { CategoryBoxArt } from "@/features/categories/components/CategoryBoxArt";

type ViewMode = "card" | "grid";
const VIEW_MODES: ViewMode[] = ["card", "grid"];

export const Route = createFileRoute("/dashboard/categories")({
	validateSearch: (search: Record<string, unknown>) => ({
		view:
			search.view === "grid" || search.view === "card"
				? (search.view as ViewMode)
				: ("card" as ViewMode),
	}),
	component: CategoriesPage,
});

function CategoriesPage() {
	const { t } = useTranslation();
	const { view } = Route.useSearch();
	const navigate = Route.useNavigate();
	const { data: categories, isLoading, error } = useCategories();

	return (
		<TitledLayout
			title={t("categories.title")}
			actions={
				<ViewDropdown
					current={view}
					onChange={(next) =>
						void navigate({ search: (s) => ({ ...s, view: next }) })
					}
				/>
			}
		>
			{isLoading && (
				<div className="text-muted-foreground">{t("common.loading")}</div>
			)}

			{error && (
				<div className="rounded-lg bg-destructive/10 p-4 text-destructive text-sm shadow-sm">
					{t("categories.failed_to_load")}: {error.message}
				</div>
			)}

			{categories && categories.length === 0 && !isLoading && !error && (
				<div className="text-muted-foreground">{t("categories.empty")}</div>
			)}

			{categories && categories.length > 0 && (
				<>
					{view === "card" ? (
						<CardGrid categories={categories} />
					) : (
						<DenseGrid categories={categories} />
					)}
				</>
			)}
		</TitledLayout>
	);
}

// Card grid — one row of boxed art + name below in a padded card.
// Suits a modest library where each category deserves attention.
function CardGrid({ categories }: { categories: CategoryResponse[] }) {
	return (
		<div className="grid grid-cols-[repeat(auto-fit,minmax(160px,1fr))] gap-4">
			{categories.map((c) => (
				<Link
					key={c.id}
					// biome-ignore lint/suspicious/noExplicitAny: param route typing
					to={"/dashboard/categories/$categoryId" as any}
					// biome-ignore lint/suspicious/noExplicitAny: param route typing
					params={{ categoryId: c.id } as any}
					className="rounded-lg bg-card overflow-hidden shadow-sm hover:ring-2 hover:ring-primary transition-all duration-75"
				>
					<CategoryBoxArt url={c.box_art_url} name={c.name} />
					<div className="p-2 text-sm font-medium truncate">{c.name}</div>
				</Link>
			))}
		</div>
	);
}

// Dense grid — v1-style: fluid auto-fit, tight gap, border-only hover on
// bare box art, name as a link-styled title underneath. Maximizes the
// number of categories on screen.
function DenseGrid({ categories }: { categories: CategoryResponse[] }) {
	return (
		<div className="grid grid-cols-[repeat(auto-fit,minmax(140px,1fr))] gap-3">
			{categories.map((c) => (
				<Link
					key={c.id}
					// biome-ignore lint/suspicious/noExplicitAny: param route typing
					to={"/dashboard/categories/$categoryId" as any}
					// biome-ignore lint/suspicious/noExplicitAny: param route typing
					params={{ categoryId: c.id } as any}
					className="group flex flex-col gap-1.5"
				>
					<CategoryBoxArt
						url={c.box_art_url}
						name={c.name}
						className="rounded-md border-4 border-background group-hover:border-primary transition-colors duration-75"
					/>
					<div className="text-sm font-medium truncate group-hover:text-link transition-colors duration-75">
						{c.name}
					</div>
				</Link>
			))}
		</div>
	);
}

function ViewDropdown({
	current,
	onChange,
}: {
	current: ViewMode;
	onChange: (mode: ViewMode) => void;
}) {
	const { t } = useTranslation();
	const labels: Record<ViewMode, string> = {
		card: t("categories.view_card"),
		grid: t("categories.view_grid"),
	};
	const icons: Record<ViewMode, React.ReactNode> = {
		card: <Rows className="size-4" />,
		grid: <SquaresFour className="size-4" />,
	};
	return (
		<DropdownMenu>
			<DropdownMenuTrigger
				render={(triggerProps) => (
					<Button variant="outline" size="sm" {...triggerProps}>
						{icons[current]}
						{labels[current]}
					</Button>
				)}
			/>
			<DropdownMenuContent>
				{VIEW_MODES.map((mode) => (
					<DropdownMenuItem key={mode} onClick={() => onChange(mode)}>
						{icons[mode]}
						{labels[mode]}
					</DropdownMenuItem>
				))}
			</DropdownMenuContent>
		</DropdownMenu>
	);
}
