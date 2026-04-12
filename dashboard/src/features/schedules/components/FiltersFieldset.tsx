import { useTranslation } from "react-i18next"
import { Checkbox } from "@/components/ui/checkbox"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import {
	MultiSelectPicker,
	type PickerOption,
} from "@/components/ui/multi-select-picker"
import { useCategories } from "@/features/categories/queries"
import { useTags } from "@/features/tags"
import { FieldError } from "./FieldError"

// FiltersFieldset renders the shared filter controls used by both the
// create and edit forms. Uses `any` on the form prop because TanStack
// Form's inferred signature depends on a closure-captured validators
// arg; typing it explicitly balloons the generic shape without payoff
// for a one-file-shared component.
// biome-ignore lint/suspicious/noExplicitAny: See above
export function FiltersFieldset({ form }: { form: any }) {
	const { t } = useTranslation()
	const categoriesQuery = useCategories()
	const tagsQuery = useTags()

	const categoryOptions: PickerOption<string>[] = (categoriesQuery.data ?? []).map(
		(c) => ({ id: c.id, label: c.name }),
	)
	const tagOptions: PickerOption<number>[] = (tagsQuery.data ?? []).map((tag) => ({
		id: tag.id,
		label: tag.name,
	}))

	return (
		<fieldset className="rounded-md border border-border bg-muted/20 p-3 space-y-3">
			<legend className="text-xs px-1 text-muted-foreground">
				{t("schedules.filters")}
			</legend>

			<form.Field name="has_min_viewers">
				{(field: any) => (
					<Label className="flex items-center gap-2 text-sm font-normal">
						<Checkbox
							checked={field.state.value}
							onCheckedChange={(c: boolean) => field.handleChange(c === true)}
						/>
						{t("schedules.has_min_viewers")}
					</Label>
				)}
			</form.Field>
			<form.Subscribe selector={(s: any) => s.values.has_min_viewers}>
				{(hasMinViewers: boolean) =>
					hasMinViewers ? (
						<form.Field name="min_viewers">
							{(field: any) => (
								<div className="flex flex-col gap-1 max-w-xs">
									<Label
										htmlFor={field.name}
										className="text-xs text-muted-foreground"
									>
										{t("schedules.min_viewers")}
									</Label>
									<Input
										id={field.name}
										type="number"
										min={0}
										value={field.state.value ?? ""}
										onChange={(e: React.ChangeEvent<HTMLInputElement>) =>
											field.handleChange(
												e.target.value === ""
													? undefined
													: Number(e.target.value),
											)
										}
										aria-invalid={
											field.state.meta.errors.length > 0 ? true : undefined
										}
									/>
									<FieldError errors={field.state.meta.errors} />
								</div>
							)}
						</form.Field>
					) : null
				}
			</form.Subscribe>

			<form.Field name="has_categories">
				{(field: any) => (
					<Label className="flex items-center gap-2 text-sm font-normal">
						<Checkbox
							checked={field.state.value}
							onCheckedChange={(c: boolean) => field.handleChange(c === true)}
						/>
						{t("schedules.has_categories")}
					</Label>
				)}
			</form.Field>
			<form.Subscribe selector={(s: any) => s.values.has_categories}>
				{(hasCategories: boolean) =>
					hasCategories ? (
						<form.Field name="category_ids">
							{(field: any) => (
								<MultiSelectPicker<string>
									options={categoryOptions}
									selected={field.state.value ?? []}
									onChange={(next) => field.handleChange(next)}
									placeholder={t("schedules.search_categories")}
									emptyHint={t("schedules.no_categories")}
								/>
							)}
						</form.Field>
					) : null
				}
			</form.Subscribe>

			<form.Field name="has_tags">
				{(field: any) => (
					<Label className="flex items-center gap-2 text-sm font-normal">
						<Checkbox
							checked={field.state.value}
							onCheckedChange={(c: boolean) => field.handleChange(c === true)}
						/>
						{t("schedules.has_tags")}
					</Label>
				)}
			</form.Field>
			<form.Subscribe selector={(s: any) => s.values.has_tags}>
				{(hasTags: boolean) =>
					hasTags ? (
						<form.Field name="tag_ids">
							{(field: any) => (
								<MultiSelectPicker<number>
									options={tagOptions}
									selected={field.state.value ?? []}
									onChange={(next) => field.handleChange(next)}
									placeholder={t("schedules.search_tags")}
									emptyHint={t("schedules.no_tags")}
								/>
							)}
						</form.Field>
					) : null
				}
			</form.Subscribe>
		</fieldset>
	)
}
