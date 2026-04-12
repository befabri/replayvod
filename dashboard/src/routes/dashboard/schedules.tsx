import { useForm } from "@tanstack/react-form"
import { createFileRoute } from "@tanstack/react-router"
import { useState } from "react"
import { useTranslation } from "react-i18next"
import { z } from "zod"
import { CreateInputSchema } from "@/api/generated/zod"
import {
	MultiSelectPicker,
	type PickerOption,
} from "@/components/MultiSelectPicker"
import { useCategories } from "@/features/categories/queries"
import type { ScheduleResponse } from "@/features/schedules"
import {
	useCreateSchedule,
	useDeleteSchedule,
	useSchedules,
	useToggleSchedule,
	useUpdateSchedule,
} from "@/features/schedules"
import { useTags } from "@/features/tags"

// ScheduleFormSchema picks the fields the form lets users edit. All
// filter dimensions from the backend CreateInput are active now:
// min_viewers (numeric threshold), plus category and tag allowlists
// driven by the shared MultiSelectPicker.
const ScheduleFormSchema = CreateInputSchema.pick({
	broadcaster_id: true,
	quality: true,
	has_min_viewers: true,
	min_viewers: true,
	has_categories: true,
	category_ids: true,
	has_tags: true,
	tag_ids: true,
}).extend({
	broadcaster_id: z
		.string()
		.min(1)
		.regex(/^\d+$/, "broadcaster_id must be numeric"),
})
	.superRefine((value, ctx) => {
		if (value.has_min_viewers && value.min_viewers == null) {
			ctx.addIssue({
				code: z.ZodIssueCode.custom,
				path: ["min_viewers"],
				message: "min_viewers is required when has_min_viewers is enabled",
			})
		}
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

	// TanStack Form wired with a Zod-validated submit. All three
	// filter dimensions (min_viewers / categories / tags) are now
	// live via Phase 6 stream enrichment + the MultiSelectPicker
	// plugged in below.
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

// FiltersFieldset renders the shared filter controls used by both the
// create and edit forms. Uses `any` on the form prop because TanStack
// Form's inferred signature depends on a closure-captured validators
// arg; typing it explicitly balloons the generic shape without payoff
// for a one-file-shared component.
// biome-ignore lint/suspicious/noExplicitAny: See above
function FiltersFieldset({ form }: { form: any }) {
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
					<label className="flex items-center gap-2 text-sm">
						<input
							type="checkbox"
							checked={field.state.value}
							onChange={(e: React.ChangeEvent<HTMLInputElement>) =>
								field.handleChange(e.target.checked)
							}
						/>
						{t("schedules.has_min_viewers")}
					</label>
				)}
			</form.Field>
			<form.Subscribe selector={(s: any) => s.values.has_min_viewers}>
				{(hasMinViewers: boolean) =>
					hasMinViewers ? (
						<form.Field name="min_viewers">
							{(field: any) => (
								<label className="flex flex-col gap-1 max-w-xs">
									<span className="text-xs text-muted-foreground">
										{t("schedules.min_viewers")}
									</span>
									<input
										type="number"
										min={0}
										value={field.state.value ?? ""}
										onChange={(
											e: React.ChangeEvent<HTMLInputElement>,
										) =>
											field.handleChange(
												e.target.value === ""
													? undefined
													: Number(e.target.value),
											)
										}
										className="rounded-md border border-border bg-background px-3 py-2 text-sm"
										aria-invalid={
											field.state.meta.errors.length > 0 ? true : undefined
										}
									/>
									<FieldError errors={field.state.meta.errors} />
								</label>
							)}
						</form.Field>
					) : null
				}
			</form.Subscribe>

			<form.Field name="has_categories">
				{(field: any) => (
					<label className="flex items-center gap-2 text-sm">
						<input
							type="checkbox"
							checked={field.state.value}
							onChange={(e: React.ChangeEvent<HTMLInputElement>) =>
								field.handleChange(e.target.checked)
							}
						/>
						{t("schedules.has_categories")}
					</label>
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
					<label className="flex items-center gap-2 text-sm">
						<input
							type="checkbox"
							checked={field.state.value}
							onChange={(e: React.ChangeEvent<HTMLInputElement>) =>
								field.handleChange(e.target.checked)
							}
						/>
						{t("schedules.has_tags")}
					</label>
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

function ScheduleRow({ schedule }: { schedule: ScheduleResponse }) {
	const { t } = useTranslation()
	const toggle = useToggleSchedule()
	const del = useDeleteSchedule()
	const [editing, setEditing] = useState(false)

	if (editing) {
		return (
			<EditForm schedule={schedule} onDone={() => setEditing(false)} />
		)
	}

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
						{schedule.has_categories &&
							schedule.categories.length > 0 && (
								<Badge>
									{t("schedules.categories")}:{" "}
									{schedule.categories.map((c) => c.name).join(", ")}
								</Badge>
							)}
						{schedule.has_tags && schedule.tags.length > 0 && (
							<Badge>
								{t("schedules.tags")}:{" "}
								{schedule.tags.map((tag) => tag.name).join(", ")}
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
						onClick={() => setEditing(true)}
						className="text-sm px-3 py-1 rounded-md border border-border hover:bg-muted"
					>
						{t("schedules.edit")}
					</button>
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

// EditForm reuses the same Zod-validated TanStack Form shape as the
// create form but submits to useUpdateSchedule. Non-editable fields
// (broadcaster_id, is_delete_rediff, is_disabled) pass through as-is
// from the current schedule so the update doesn't silently reset
// operational state the user didn't touch.
function EditForm({
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
			tag_ids: schedule.tags.map((t) => t.id),
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
				<form.Subscribe selector={(s) => [s.canSubmit, s.isSubmitting] as const}>
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
