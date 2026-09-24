import { useForm } from "@tanstack/react-form";
import { useEffect } from "react";
import { useTranslation } from "react-i18next";
import type { z } from "zod";
import { SettingsUpdateInputSchema } from "@/api/generated/zod";
import { Alert } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { LoadingState } from "@/components/ui/loading-state";
import {
	Select,
	SelectContent,
	SelectItem,
	SelectTrigger,
	SelectValue,
} from "@/components/ui/select";
import { ButtonSkeleton, FieldSkeleton } from "@/components/ui/skeleton";
import { type SettingsResponse, useUpdateSettings } from "@/features/settings";

type SettingsFormValues = z.infer<typeof SettingsUpdateInputSchema>;
type DateTimeFormat = SettingsFormValues["datetime_format"];
type SettingsLanguage = SettingsFormValues["language"];

const DATE_TIME_FORMATS: readonly DateTimeFormat[] = ["ISO", "EU", "US"];
const SETTINGS_LANGUAGES: readonly SettingsLanguage[] = ["en", "fr"];
const FIELDS = ["timezone", "datetime_format", "language"] as const;
const FORM_CLASS = "rounded-lg border border-border bg-card p-6 space-y-4";

function isDateTimeFormat(value: unknown): value is DateTimeFormat {
	return DATE_TIME_FORMATS.some((format) => format === value);
}

function isSettingsLanguage(value: unknown): value is SettingsLanguage {
	return SETTINGS_LANGUAGES.some((language) => language === value);
}

function settingsFormValues(data: SettingsResponse): SettingsFormValues {
	return {
		timezone: data.timezone || "UTC",
		datetime_format: isDateTimeFormat(data.datetime_format)
			? data.datetime_format
			: "ISO",
		language: isSettingsLanguage(data.language) ? data.language : "en",
	};
}

export function SettingsForm({ data }: { data: SettingsResponse }) {
	const { t } = useTranslation();
	const update = useUpdateSettings();

	const form = useForm({
		defaultValues: settingsFormValues(data),
		validators: {
			onSubmit: SettingsUpdateInputSchema,
		},
		onSubmit: async ({ value, formApi }) => {
			const submitted = { ...value };
			try {
				const saved = await update.mutateAsync(value);
				if (
					FIELDS.every((name) => formApi.state.values[name] === submitted[name])
				) {
					formApi.reset(settingsFormValues(saved));
				}
			} catch {}
		},
	});
	useEffect(() => {
		if (!form.state.isDirty) form.reset(settingsFormValues(data));
	}, [data, form]);

	return (
		<form
			onSubmit={(e) => {
				e.preventDefault();
				e.stopPropagation();
				void form.handleSubmit();
			}}
			className={FORM_CLASS}
		>
			<form.Field name="timezone">
				{(field) => (
					<div className="flex flex-col gap-1">
						<Label htmlFor={field.name}>{t("settings.timezone")}</Label>
						<Input
							id={field.name}
							type="text"
							value={field.state.value}
							onChange={(e) => field.handleChange(e.target.value)}
							onBlur={field.handleBlur}
							placeholder="Europe/Paris"
						/>
						<span className="text-xs text-muted-foreground">
							{t("settings.timezone_hint")}
						</span>
					</div>
				)}
			</form.Field>

			<form.Field name="datetime_format">
				{(field) => (
					<div className="flex flex-col gap-1">
						<Label htmlFor={field.name}>{t("settings.datetime_format")}</Label>
						<Select
							value={field.state.value}
							onValueChange={(v) => {
								if (typeof v === "string" && isDateTimeFormat(v)) {
									field.handleChange(v);
								}
							}}
						>
							<SelectTrigger id={field.name}>
								<SelectValue />
							</SelectTrigger>
							<SelectContent>
								<SelectItem value="ISO">ISO (2026-04-12 15:30)</SelectItem>
								<SelectItem value="EU">EU (12/04/2026 15:30)</SelectItem>
								<SelectItem value="US">US (04/12/2026 3:30 PM)</SelectItem>
							</SelectContent>
						</Select>
					</div>
				)}
			</form.Field>

			<form.Field name="language">
				{(field) => (
					<div className="flex flex-col gap-1">
						<Label htmlFor={field.name}>{t("settings.language")}</Label>
						<Select
							value={field.state.value}
							onValueChange={(v) => {
								if (typeof v === "string" && isSettingsLanguage(v)) {
									field.handleChange(v);
								}
							}}
						>
							<SelectTrigger id={field.name}>
								<SelectValue />
							</SelectTrigger>
							<SelectContent>
								<SelectItem value="en">English</SelectItem>
								<SelectItem value="fr">Français</SelectItem>
							</SelectContent>
						</Select>
					</div>
				)}
			</form.Field>

			{update.isError && (
				<Alert variant="destructive">
					{update.error?.message ?? t("settings.save_failed")}
				</Alert>
			)}

			<form.Subscribe selector={(state) => state.isDirty}>
				{(dirty) =>
					update.isSuccess &&
					!dirty && <Alert variant="success">{t("settings.saved")}</Alert>
				}
			</form.Subscribe>

			<form.Subscribe selector={(s) => [s.canSubmit, s.isSubmitting] as const}>
				{([canSubmit, isSubmitting]) => (
					<Button
						type="submit"
						disabled={!canSubmit || isSubmitting || update.isPending}
					>
						{isSubmitting || update.isPending
							? t("common.saving")
							: t("settings.save")}
					</Button>
				)}
			</form.Subscribe>
		</form>
	);
}

export function SettingsFormSkeleton() {
	const { t } = useTranslation();
	return (
		<LoadingState>
			<div className={FORM_CLASS}>
				<div className="flex flex-col gap-1">
					<Label>{t("settings.timezone")}</Label>
					<FieldSkeleton />
					<span className="text-xs text-muted-foreground">
						{t("settings.timezone_hint")}
					</span>
				</div>
				<div className="flex flex-col gap-1">
					<Label>{t("settings.datetime_format")}</Label>
					<FieldSkeleton />
				</div>
				<div className="flex flex-col gap-1">
					<Label>{t("settings.language")}</Label>
					<FieldSkeleton />
				</div>
				<ButtonSkeleton />
			</div>
		</LoadingState>
	);
}
