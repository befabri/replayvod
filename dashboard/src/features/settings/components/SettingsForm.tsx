import { useForm } from "@tanstack/react-form";
import { useEffect } from "react";
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

export function SettingsForm({ data }: { data: SettingsResponse }) {
	const { t } = useTranslation();
	const update = useUpdateSettings();

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
			await update.mutateAsync(value);
		},
	});

	// Sync defaults into the form once settings load. Without this,
	// the form mounts with placeholder defaults and a user submit
	// would overwrite their server-side values with defaults.
	useEffect(() => {
		form.reset({
			timezone: data.timezone,
			datetime_format:
				data.datetime_format as SettingsFormValues["datetime_format"],
			language: data.language as SettingsFormValues["language"],
		});
	}, [data, form]);

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
							onValueChange={(v) =>
								field.handleChange(v as SettingsFormValues["datetime_format"])
							}
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
							onValueChange={(v) =>
								field.handleChange(v as SettingsFormValues["language"])
							}
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
