import { useForm } from "@tanstack/react-form"
import { useTranslation } from "react-i18next"
import { useCreateSchedule } from "@/features/schedules/queries"
import {
	ScheduleFormSchema,
	type ScheduleFormValues,
} from "@/features/schedules/schema"
import { FieldError } from "./FieldError"
import { FiltersFieldset } from "./FiltersFieldset"

export function CreateForm() {
	const { t } = useTranslation()
	const create = useCreateSchedule()

	const form = useForm({
		defaultValues: {
			broadcaster_id: "",
			quality: "HIGH",
			has_min_viewers: false,
			min_viewers: undefined,
			has_categories: false,
			category_ids: [],
			has_tags: false,
			tag_ids: [],
		} as ScheduleFormValues,
		validators: {
			onSubmit: ScheduleFormSchema,
		},
		onSubmit: async ({ value, formApi }) => {
			await create.mutateAsync({
				broadcaster_id: value.broadcaster_id.trim(),
				quality: value.quality,
				has_min_viewers: value.has_min_viewers,
				min_viewers: value.has_min_viewers ? value.min_viewers : undefined,
				has_categories: value.has_categories,
				has_tags: value.has_tags,
				is_delete_rediff: false,
				is_disabled: false,
				category_ids: value.has_categories ? value.category_ids : [],
				tag_ids: value.has_tags ? value.tag_ids : [],
			})
			formApi.reset()
		},
	})

	return (
		<form
			onSubmit={(e) => {
				e.preventDefault()
				e.stopPropagation()
				void form.handleSubmit()
			}}
			className="rounded-lg border border-border bg-card p-4 mb-6 space-y-3"
		>
			<h2 className="text-lg font-medium">{t("schedules.create_title")}</h2>
			<div className="grid grid-cols-1 sm:grid-cols-2 gap-3">
				<form.Field name="broadcaster_id">
					{(field) => (
						<label className="flex flex-col gap-1">
							<span className="text-sm text-muted-foreground">
								{t("schedules.broadcaster_id")}
							</span>
							<input
								id={field.name}
								name={field.name}
								type="text"
								value={field.state.value}
								onChange={(e) => field.handleChange(e.target.value)}
								onBlur={field.handleBlur}
								placeholder="12345"
								className="rounded-md border border-border bg-background px-3 py-2 text-sm"
								aria-invalid={
									field.state.meta.errors.length > 0 ? true : undefined
								}
							/>
							<FieldError errors={field.state.meta.errors} />
						</label>
					)}
				</form.Field>
				<form.Field name="quality">
					{(field) => (
						<label className="flex flex-col gap-1">
							<span className="text-sm text-muted-foreground">
								{t("schedules.quality")}
							</span>
							<select
								id={field.name}
								name={field.name}
								value={field.state.value}
								onChange={(e) =>
									field.handleChange(
										e.target.value as ScheduleFormValues["quality"],
									)
								}
								onBlur={field.handleBlur}
								className="rounded-md border border-border bg-background px-3 py-2 text-sm"
							>
								<option value="HIGH">{t("schedules.quality_high")}</option>
								<option value="MEDIUM">{t("schedules.quality_medium")}</option>
								<option value="LOW">{t("schedules.quality_low")}</option>
							</select>
						</label>
					)}
				</form.Field>
			</div>

			<FiltersFieldset form={form} />

			{create.isError && (
				<div className="rounded-md bg-destructive/10 border border-destructive/20 p-3 text-destructive text-sm">
					{create.error?.message ?? t("schedules.create_failed")}
				</div>
			)}

			<form.Subscribe selector={(s) => [s.canSubmit, s.isSubmitting] as const}>
				{([canSubmit, isSubmitting]) => (
					<button
						type="submit"
						disabled={!canSubmit || isSubmitting || create.isPending}
						className="rounded-md bg-primary px-4 py-2 text-sm font-medium text-primary-foreground disabled:opacity-60"
					>
						{isSubmitting || create.isPending
							? t("common.saving")
							: t("schedules.create_submit")}
					</button>
				)}
			</form.Subscribe>
		</form>
	)
}
