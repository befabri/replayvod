import { useForm } from "@tanstack/react-form"
import { useTranslation } from "react-i18next"
import type { ScheduleResponse } from "@/features/schedules"
import { useUpdateSchedule } from "@/features/schedules/queries"
import {
	ScheduleFormSchema,
	type ScheduleFormValues,
} from "@/features/schedules/schema"
import { FiltersFieldset } from "./FiltersFieldset"

// EditForm reuses the same Zod-validated TanStack Form shape as the
// create form but submits to useUpdateSchedule. Non-editable fields
// (broadcaster_id, is_delete_rediff, is_disabled) pass through as-is
// from the current schedule so the update doesn't silently reset
// operational state the user didn't touch.
export function EditForm({
	schedule,
	onDone,
}: {
	schedule: ScheduleResponse
	onDone: () => void
}) {
	const { t } = useTranslation()
	const update = useUpdateSchedule()

	const form = useForm({
		defaultValues: {
			broadcaster_id: schedule.broadcaster_id,
			quality: schedule.quality as ScheduleFormValues["quality"],
			has_min_viewers: schedule.has_min_viewers,
			min_viewers: schedule.min_viewers ?? undefined,
			has_categories: schedule.has_categories,
			category_ids: schedule.categories.map((c) => c.id),
			has_tags: schedule.has_tags,
			tag_ids: schedule.tags.map((tag) => tag.id),
		} as ScheduleFormValues,
		validators: {
			onSubmit: ScheduleFormSchema,
		},
		onSubmit: async ({ value }) => {
			await update.mutateAsync({
				id: schedule.id,
				quality: value.quality,
				has_min_viewers: value.has_min_viewers,
				min_viewers: value.has_min_viewers ? value.min_viewers : undefined,
				has_categories: value.has_categories,
				has_tags: value.has_tags,
				is_delete_rediff: schedule.is_delete_rediff,
				time_before_delete: schedule.time_before_delete,
				is_disabled: schedule.is_disabled,
				category_ids: value.has_categories ? value.category_ids : [],
				tag_ids: value.has_tags ? value.tag_ids : [],
			})
			onDone()
		},
	})

	return (
		<form
			onSubmit={(e) => {
				e.preventDefault()
				e.stopPropagation()
				void form.handleSubmit()
			}}
			className="rounded-lg border border-primary/40 bg-card p-4 space-y-3"
		>
			<div className="flex items-center justify-between">
				<h3 className="text-sm font-medium">
					{t("schedules.edit_title")}
					<span className="ml-2 font-mono text-xs text-muted-foreground">
						{schedule.broadcaster_id}
					</span>
				</h3>
			</div>
			<form.Field name="quality">
				{(field) => (
					<label className="flex flex-col gap-1 max-w-xs">
						<span className="text-xs text-muted-foreground">
							{t("schedules.quality")}
						</span>
						<select
							value={field.state.value}
							onChange={(e) =>
								field.handleChange(
									e.target.value as ScheduleFormValues["quality"],
								)
							}
							className="rounded-md border border-border bg-background px-3 py-2 text-sm"
						>
							<option value="HIGH">{t("schedules.quality_high")}</option>
							<option value="MEDIUM">{t("schedules.quality_medium")}</option>
							<option value="LOW">{t("schedules.quality_low")}</option>
						</select>
					</label>
				)}
			</form.Field>
			<FiltersFieldset form={form} />

			{update.isError && (
				<div className="rounded-md bg-destructive/10 border border-destructive/20 p-3 text-destructive text-sm">
					{update.error?.message ?? t("schedules.update_failed")}
				</div>
			)}

			<div className="flex gap-2">
				<form.Subscribe
					selector={(s) => [s.canSubmit, s.isSubmitting] as const}
				>
					{([canSubmit, isSubmitting]) => (
						<button
							type="submit"
							disabled={!canSubmit || isSubmitting || update.isPending}
							className="rounded-md bg-primary px-4 py-2 text-sm font-medium text-primary-foreground disabled:opacity-60"
						>
							{isSubmitting || update.isPending
								? t("common.saving")
								: t("schedules.save")}
						</button>
					)}
				</form.Subscribe>
				<button
					type="button"
					onClick={onDone}
					className="rounded-md border border-border px-4 py-2 text-sm"
				>
					{t("schedules.cancel")}
				</button>
			</div>
		</form>
	)
}
