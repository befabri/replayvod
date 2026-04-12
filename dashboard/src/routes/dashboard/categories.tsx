import { createFileRoute } from "@tanstack/react-router"
import { useTranslation } from "react-i18next"
import { useCategories } from "@/features/categories"

export const Route = createFileRoute("/dashboard/categories")({
	component: CategoriesPage,
})

function CategoriesPage() {
	const { t } = useTranslation()
	const { data: categories, isLoading, error } = useCategories()

	return (
		<div className="p-8">
			<h1 className="text-3xl font-heading font-bold mb-6">
				{t("nav.categories")}
			</h1>

			{isLoading && (
				<div className="text-muted-foreground">Loading categories…</div>
			)}

			{error && (
				<div className="rounded-md bg-destructive/10 border border-destructive/20 p-4 text-destructive text-sm">
					Failed to load categories: {error.message}
				</div>
			)}

			{categories && categories.length === 0 && (
				<div className="text-muted-foreground">
					No categories yet. Categories will be populated as streams are
					recorded.
				</div>
			)}

			{categories && categories.length > 0 && (
				<div className="grid grid-cols-2 sm:grid-cols-3 md:grid-cols-4 lg:grid-cols-6 gap-4">
					{categories.map((c) => (
						<div
							key={c.id}
							className="rounded-lg border border-border bg-card overflow-hidden"
						>
							{c.box_art_url && (
								<img
									src={c.box_art_url.replace("{width}", "144").replace("{height}", "192")}
									alt={c.name}
									className="w-full aspect-[3/4] object-cover"
								/>
							)}
							<div className="p-2 text-sm font-medium truncate">{c.name}</div>
						</div>
					))}
				</div>
			)}
		</div>
	)
}
