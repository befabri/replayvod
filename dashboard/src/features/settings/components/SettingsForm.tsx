import { useForm } from "@tanstack/react-form";
import { useTranslation } from "react-i18next";
import type { z } from "zod";
import { SettingsUpdateInputSchema } from "@/api/generated/zod";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import {
	Select,
	SelectContent,
	SelectItem,
	SelectTrigger,
	SelectValue,
} from "@/components/ui/select";
import { type SettingsResponse, useUpdateSettings } from "@/features/settings";

type SettingsFormValues = z.infer<typeof SettingsUpdateInputSchema>;
type DateTimeFormat = SettingsFormValues["datetime_format"];
type SettingsLanguage = SettingsFormValues["language"];

const DATE_TIME_FORMATS: readonly DateTimeFormat[] = ["ISO", "EU", "US"];
const SETTINGS_LANGUAGES: readonly SettingsLanguage[] = ["en", "fr"];

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

	// Defaults are computed from the loaded settings at first render — the parent
	// only mounts this form once `data` is present — so there's no prop-to-state
	// sync effect. Successful saves re-baseline from the mutation response below.
	const form = useForm({
		defaultValues: settingsFormValues(data),
		validators: {
			onSubmit: SettingsUpdateInputSchema,
		},
		onSubmit: async ({ value, formApi }) => {
			const saved = await update.mutateAsync(value);
			formApi.reset(settingsFormValues(saved));
		},
	});

	return (
		<form
			onSubmit={(e) => {
				e.preventDefault();
				e.stopPropagation();
				void form.handleSubmit();
			}}
			className="rounded-lg border border-border bg-card p-6 space-y-4"
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
					<div className="flex flex-col gap-1 max-w-xs">
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
					<div className="flex flex-col gap-1 max-w-xs">
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
