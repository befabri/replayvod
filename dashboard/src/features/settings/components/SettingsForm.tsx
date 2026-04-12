import { useForm } from "@tanstack/react-form"
import { useEffect } from "react"
import { useTranslation } from "react-i18next"
import type { z } from "zod"
import { SettingsUpdateInputSchema } from "@/api/generated/zod"
import {
	type SettingsResponse,
	useUpdateSettings,
} from "@/features/settings"

type SettingsFormValues = z.infer<typeof SettingsUpdateInputSchema>

export function SettingsForm({ data }: { data: SettingsResponse }) {
	const { t } = useTranslation()
	const update = useUpdateSettings()

	const form = useForm({
		defaultValues: {
			timezone: "UTC",
			datetime_format: "ISO",
			language: "en",
		} as SettingsFormValues,
		validators: {
			onSubmit: SettingsUpdateInputSchema,
		},
		onSubmit: async ({ value }) => {
			await update.mutateAsync(value)
		},
	})

	// Sync defaults into the form once settings load. Without this,
	// the form mounts with placeholder defaults and a user submit
	// would overwrite their server-side values with defaults.
	useEffect(() => {
		form.reset({
			timezone: data.timezone,
			datetime_format: data.datetime_format as SettingsFormValues["datetime_format"],
			language: data.language as SettingsFormValues["language"],
		})
	}, [data, form])

	return (
		<form
			onSubmit={(e) => {
				e.preventDefault()
				e.stopPropagation()
				void form.handleSubmit()
			}}
			className="rounded-lg border border-border bg-card p-6 space-y-4"
		>
			<form.Field name="timezone">
				{(field) => (
					<label className="flex flex-col gap-1">
						<span className="text-sm font-medium">
							{t("settings.timezone")}
						</span>
						<input
							id={field.name}
							type="text"
							value={field.state.value}
							onChange={(e) => field.handleChange(e.target.value)}
							onBlur={field.handleBlur}
							placeholder="Europe/Paris"
							className="rounded-md border border-border bg-background px-3 py-2 text-sm"
						/>
						<span className="text-xs text-muted-foreground">
							{t("settings.timezone_hint")}
						</span>
					</label>
				)}
			</form.Field>

			<form.Field name="datetime_format">
				{(field) => (
					<label className="flex flex-col gap-1">
						<span className="text-sm font-medium">
							{t("settings.datetime_format")}
						</span>
						<select
							id={field.name}
							value={field.state.value}
							onChange={(e) =>
								field.handleChange(
									e.target.value as SettingsFormValues["datetime_format"],
								)
							}
							onBlur={field.handleBlur}
							className="rounded-md border border-border bg-background px-3 py-2 text-sm max-w-xs"
						>
							<option value="ISO">ISO (2026-04-12 15:30)</option>
							<option value="EU">EU (12/04/2026 15:30)</option>
							<option value="US">US (04/12/2026 3:30 PM)</option>
						</select>
					</label>
				)}
			</form.Field>

			<form.Field name="language">
				{(field) => (
					<label className="flex flex-col gap-1">
						<span className="text-sm font-medium">
							{t("settings.language")}
						</span>
						<select
							id={field.name}
							value={field.state.value}
							onChange={(e) =>
								field.handleChange(
									e.target.value as SettingsFormValues["language"],
								)
							}
							onBlur={field.handleBlur}
							className="rounded-md border border-border bg-background px-3 py-2 text-sm max-w-xs"
						>
							<option value="en">English</option>
							<option value="fr">Français</option>
						</select>
					</label>
				)}
			</form.Field>

			{update.isError && (
				<div className="rounded-md bg-destructive/10 border border-destructive/20 p-3 text-destructive text-sm">
					{update.error?.message ?? t("settings.save_failed")}
				</div>
			)}

			{update.isSuccess && (
				<div className="rounded-md bg-primary/10 border border-primary/20 p-3 text-sm">
					{t("settings.saved")}
				</div>
			)}

			<form.Subscribe selector={(s) => [s.canSubmit, s.isSubmitting] as const}>
				{([canSubmit, isSubmitting]) => (
					<button
						type="submit"
						disabled={!canSubmit || isSubmitting || update.isPending}
						className="rounded-md bg-primary px-4 py-2 text-sm font-medium text-primary-foreground disabled:opacity-60"
					>
						{isSubmitting || update.isPending
							? t("common.saving")
							: t("settings.save")}
					</button>
				)}
			</form.Subscribe>
		</form>
	)
}
