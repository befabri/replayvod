import { useForm } from "@tanstack/react-form"
import { useTranslation } from "react-i18next"
import { Button } from "@/components/ui/button"
import { Label } from "@/components/ui/label"
import {
	Select,
	SelectContent,
	SelectItem,
	SelectTrigger,
	SelectValue,
} from "@/components/ui/select"
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
					<div className="flex flex-col gap-1 max-w-xs">
						<Label htmlFor={field.name} className="text-xs text-muted-foreground">
							{t("schedules.quality")}
						</Label>
						<Select
							value={field.state.value}
							onValueChange={(v) =>
								field.handleChange(v as ScheduleFormValues["quality"])
							}
						>
							<SelectTrigger id={field.name}>
								<SelectValue />
							</SelectTrigger>
							<SelectContent>
								<SelectItem value="HIGH">
									{t("schedules.quality_high")}
								</SelectItem>
								<SelectItem value="MEDIUM">
									{t("schedules.quality_medium")}
								</SelectItem>
								<SelectItem value="LOW">{t("schedules.quality_low")}</SelectItem>
							</SelectContent>
						</Select>
					</div>
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
						<Button
							type="submit"
							disabled={!canSubmit || isSubmitting || update.isPending}
						>
							{isSubmitting || update.isPending
								? t("common.saving")
								: t("schedules.save")}
						</Button>
					)}
				</form.Subscribe>
				<Button type="button" variant="outline" onClick={onDone}>
					{t("schedules.cancel")}
				</Button>
			</div>
		</form>
	)
}
