import { useForm } from "@tanstack/react-form"
import { createFileRoute } from "@tanstack/react-router"
import { useTranslation } from "react-i18next"
import { z } from "zod"
import { CreateInputSchema } from "@/api/generated/zod"
import type { ScheduleResponse } from "@/features/schedules"
import {
	useCreateSchedule,
	useDeleteSchedule,
	useSchedules,
	useToggleSchedule,
} from "@/features/schedules"

// Shrink the generated Zod schema to the fields the form actually lets
// users edit today. The filter toggles are deferred (see "coming soon"
// fieldset) so they stay hardcoded `false` at submit time instead of
// showing up in the form state. Once Phase 6's stream-signal enrichment
// lands, widen this schema and the form will pick up the new fields.
const ScheduleFormSchema = CreateInputSchema.pick({
	broadcaster_id: true,
	quality: true,
}).extend({
	broadcaster_id: z
		.string()
		.min(1)
		.regex(/^\d+$/, "broadcaster_id must be numeric"),
})

type ScheduleFormValues = z.infer<typeof ScheduleFormSchema>

export const Route = createFileRoute("/dashboard/schedules")({
	component: SchedulesPage,
})

function SchedulesPage() {
	const { t } = useTranslation()
	const { data, isLoading, error } = useSchedules()

	return (
		<div className="p-8 max-w-4xl">
			<h1 className="text-3xl font-heading font-bold mb-2">
				{t("schedules.title")}
			</h1>
			<p className="text-sm text-muted-foreground mb-6">
				{t("schedules.description")}
			</p>

			<CreateForm />

			{isLoading && (
				<div className="text-muted-foreground">{t("common.loading")}</div>
			)}
			{error && (
				<div className="rounded-md bg-destructive/10 border border-destructive/20 p-4 text-destructive text-sm">
					{t("schedules.failed_to_load")}: {error.message}
				</div>
			)}
			{data && data.data.length === 0 && !isLoading && !error && (
				<div className="text-muted-foreground mt-8">
					{t("schedules.empty")}
				</div>
			)}

			{data && data.data.length > 0 && (
				<div className="mt-8 space-y-3">
					{data.data.map((s) => (
						<ScheduleRow key={s.id} schedule={s} />
					))}
				</div>
			)}
		</div>
	)
}

function CreateForm() {
	const { t } = useTranslation()
	const create = useCreateSchedule()

	// TanStack Form wired with a Zod-validated submit. Viewer-count /
	// category / tag filters stay hardcoded false here because the
	// stream.online webhook doesn't carry those signals yet — when Phase 6
	// enriches via GetStreams, widen ScheduleFormSchema to pick them up.
	const form = useForm({
		defaultValues: {
			broadcaster_id: "",
			quality: "HIGH",
		} as ScheduleFormValues,
		validators: {
			onSubmit: ScheduleFormSchema,
		},
		onSubmit: async ({ value, formApi }) => {
			await create.mutateAsync({
				broadcaster_id: value.broadcaster_id.trim(),
				quality: value.quality,
				has_min_viewers: false,
				has_categories: false,
				has_tags: false,
				is_delete_rediff: false,
				is_disabled: false,
				category_ids: [],
				tag_ids: [],
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

			<fieldset className="rounded-md border border-border bg-muted/20 p-3 space-y-2">
				<legend className="text-xs px-1 text-muted-foreground">
					{t("schedules.filters_coming_soon")}
				</legend>
				<DisabledFilter label={t("schedules.has_min_viewers")} />
				<DisabledFilter label={t("schedules.has_categories")} />
				<DisabledFilter label={t("schedules.has_tags")} />
			</fieldset>

			{create.isError && (
				<div className="rounded-md bg-destructive/10 border border-destructive/20 p-3 text-destructive text-sm">
					{create.error?.message ?? t("schedules.create_failed")}
				</div>
			)}

			<form.Subscribe
				selector={(s) => [s.canSubmit, s.isSubmitting] as const}
			>
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

// FieldError renders the first validation error TanStack Form surfaces.
// Errors come through as Zod issues — stringifying is enough for the
// homelab UI; internationalized field errors would layer on top.
function FieldError({ errors }: { errors: readonly unknown[] }) {
	if (errors.length === 0) return null
	const first = errors[0]
	const msg =
		typeof first === "string"
			? first
			: first && typeof first === "object" && "message" in first
				? String((first as { message: unknown }).message)
				: "Invalid"
	return <span className="text-xs text-destructive">{msg}</span>
}

function DisabledFilter({ label }: { label: string }) {
	const { t } = useTranslation()
	return (
		<label
			className="flex items-center gap-2 text-sm opacity-60 cursor-not-allowed"
			title={t("schedules.filters_coming_soon_hint")}
		>
			<input type="checkbox" disabled />
			<span>{label}</span>
			<span className="ml-auto rounded-md bg-muted px-2 py-0.5 text-xs">
				{t("schedules.coming_soon")}
			</span>
		</label>
	)
}

function ScheduleRow({ schedule }: { schedule: ScheduleResponse }) {
	const { t } = useTranslation()
	const toggle = useToggleSchedule()
	const del = useDeleteSchedule()

	return (
		<div className="rounded-lg border border-border bg-card p-4">
			<div className="flex items-start justify-between gap-4">
				<div className="flex-1 min-w-0">
					<div className="font-mono text-sm text-muted-foreground mb-1">
						{schedule.broadcaster_id}
					</div>
					<div className="flex flex-wrap items-center gap-2 text-sm">
						<Badge>{t(`videos.quality`)}: {schedule.quality}</Badge>
						{schedule.has_min_viewers && schedule.min_viewers != null && (
							<Badge>
								{t("schedules.min_viewers")}: {schedule.min_viewers}
							</Badge>
						)}
						{schedule.is_disabled ? (
							<Badge variant="muted">{t("schedules.disabled")}</Badge>
						) : (
							<Badge variant="success">{t("schedules.enabled")}</Badge>
						)}
					</div>
					<div className="mt-2 text-xs text-muted-foreground">
						{t("schedules.triggered_count", { count: schedule.trigger_count })}
						{schedule.last_triggered_at && (
							<>
								{" · "}
								{t("schedules.last_triggered")}:{" "}
								{new Date(schedule.last_triggered_at).toLocaleString()}
							</>
						)}
					</div>
				</div>
				<div className="flex flex-col gap-2 items-end">
					<button
						type="button"
						onClick={() => toggle.mutate({ id: schedule.id })}
						disabled={toggle.isPending}
						className="text-sm px-3 py-1 rounded-md border border-border hover:bg-muted disabled:opacity-60"
					>
						{schedule.is_disabled
							? t("schedules.enable")
							: t("schedules.disable")}
					</button>
					<button
						type="button"
						onClick={() => del.mutate({ id: schedule.id })}
						disabled={del.isPending}
						className="text-sm text-destructive hover:underline disabled:opacity-60"
					>
						{t("schedules.delete")}
					</button>
				</div>
			</div>
		</div>
	)
}

function Badge({
	children,
	variant = "default",
}: {
	children: React.ReactNode
	variant?: "default" | "muted" | "success"
}) {
	const cls = {
		default: "bg-muted text-foreground",
		muted: "bg-muted text-muted-foreground",
		success: "bg-primary/20 text-primary-foreground",
	}[variant]
	return (
		<span className={`inline-flex items-center rounded-md px-2 py-0.5 text-xs ${cls}`}>
			{children}
		</span>
	)
}
