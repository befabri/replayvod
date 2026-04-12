import { useForm } from "@tanstack/react-form"
import { useTranslation } from "react-i18next"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import {
	Select,
	SelectContent,
	SelectItem,
	SelectTrigger,
	SelectValue,
} from "@/components/ui/select"
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
						<div className="flex flex-col gap-1">
							<Label htmlFor={field.name} className="text-muted-foreground">
								{t("schedules.broadcaster_id")}
							</Label>
							<Input
								id={field.name}
								name={field.name}
								type="text"
								value={field.state.value}
								onChange={(e) => field.handleChange(e.target.value)}
								onBlur={field.handleBlur}
								placeholder="12345"
								aria-invalid={
									field.state.meta.errors.length > 0 ? true : undefined
								}
							/>
							<FieldError errors={field.state.meta.errors} />
						</div>
					)}
				</form.Field>
				<form.Field name="quality">
					{(field) => (
						<div className="flex flex-col gap-1">
							<Label htmlFor={field.name} className="text-muted-foreground">
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
									<SelectItem value="LOW">
										{t("schedules.quality_low")}
									</SelectItem>
								</SelectContent>
							</Select>
						</div>
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
					<Button
						type="submit"
						disabled={!canSubmit || isSubmitting || create.isPending}
					>
						{isSubmitting || create.isPending
							? t("common.saving")
							: t("schedules.create_submit")}
					</Button>
				)}
			</form.Subscribe>
		</form>
	)
}
