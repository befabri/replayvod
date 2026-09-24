import { useForm } from "@tanstack/react-form";
import { useEffect } from "react";
import { useTranslation } from "react-i18next";
import type { SettingsResponse } from "@/api/generated/trpc";
import { UpdatePlaybackInputSchema } from "@/api/generated/zod";
import { Alert } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { LoadingState } from "@/components/ui/loading-state";
import { ButtonSkeleton, FieldSkeleton } from "@/components/ui/skeleton";
import { useUpdatePlaybackSettings } from "../queries";

const FIELDS = [
	"resume_min_seconds",
	"resume_end_margin_seconds",
	"resume_end_margin_percent",
] as const;
const FORM_CLASS = "rounded-lg border border-border bg-card p-6 space-y-4";

export function PlaybackSettingsForm({ data }: { data: SettingsResponse }) {
	const { t } = useTranslation();
	const update = useUpdatePlaybackSettings();
	const form = useForm({
		defaultValues: data.playback,
		validators: { onSubmit: UpdatePlaybackInputSchema },
		onSubmit: async ({ value, formApi }) => {
			const submitted = { ...value };
			try {
				const saved = await update.mutateAsync(value);
				if (
					FIELDS.every((name) =>
						Object.is(formApi.state.values[name], submitted[name]),
					)
				) {
					formApi.reset(saved.playback);
				}
			} catch {}
		},
	});
	useEffect(() => {
		if (!form.state.isDirty) form.reset(data.playback);
	}, [data.playback, form]);
	return (
		<form
			className={FORM_CLASS}
			aria-labelledby="playback-settings-heading"
			onSubmit={(event) => {
				event.preventDefault();
				void form.handleSubmit();
			}}
		>
			<div>
				<h2 id="playback-settings-heading" className="text-lg font-medium">
					{t("settings.playback_title")}
				</h2>
				<p className="text-sm text-muted-foreground">
					{t("settings.playback_description")}
				</p>
			</div>
			<div className="grid gap-4 md:grid-cols-3">
				{FIELDS.map((name) => (
					<form.Field key={name} name={name}>
						{(field) => {
							const schema = UpdatePlaybackInputSchema.shape[name];
							return (
								<div className="flex flex-col gap-1">
									<Label htmlFor={name}>{t(`settings.${name}`)}</Label>
									<Input
										id={name}
										type="number"
										min={schema.minValue ?? undefined}
										max={schema.maxValue ?? undefined}
										step={1}
										value={
											Number.isFinite(field.state.value)
												? field.state.value
												: ""
										}
										onChange={(event) =>
											field.handleChange(event.target.valueAsNumber)
										}
										onBlur={field.handleBlur}
										aria-describedby={`${name}-hint`}
										aria-invalid={field.state.meta.errors.length > 0}
									/>
									<p
										id={`${name}-hint`}
										className="text-xs text-muted-foreground"
									>
										{t(`settings.${name}_hint`)}
									</p>
									{field.state.meta.errors.length > 0 && (
										<p role="alert" className="text-sm text-destructive">
											{field.state.meta.errors
												.map((error) => error?.message)
												.join(" ")}
										</p>
									)}
								</div>
							);
						}}
					</form.Field>
				))}
			</div>
			{update.isError && (
				<Alert variant="destructive">{update.error.message}</Alert>
			)}
			<form.Subscribe selector={(state) => state.isDirty}>
				{(dirty) =>
					update.isSuccess &&
					!dirty && <Alert variant="success">{t("settings.saved")}</Alert>
				}
			</form.Subscribe>
			<form.Subscribe
				selector={(state) => [state.canSubmit, state.isSubmitting] as const}
			>
				{([canSubmit, submitting]) => (
					<Button
						type="submit"
						disabled={!canSubmit || submitting || update.isPending}
					>
						{submitting ? t("common.saving") : t("settings.save")}
					</Button>
				)}
			</form.Subscribe>
		</form>
	);
}

export function PlaybackSettingsFormSkeleton() {
	const { t } = useTranslation();
	return (
		<LoadingState>
			<div className={FORM_CLASS}>
				<div>
					<h2 className="text-lg font-medium">
						{t("settings.playback_title")}
					</h2>
					<p className="text-sm text-muted-foreground">
						{t("settings.playback_description")}
					</p>
				</div>
				<div className="grid gap-4 md:grid-cols-3">
					{FIELDS.map((name) => (
						<div key={name} className="flex flex-col gap-1">
							<Label>{t(`settings.${name}`)}</Label>
							<FieldSkeleton />
							<p className="text-xs text-muted-foreground">
								{t(`settings.${name}_hint`)}
							</p>
						</div>
					))}
				</div>
				<ButtonSkeleton />
			</div>
		</LoadingState>
	);
}
