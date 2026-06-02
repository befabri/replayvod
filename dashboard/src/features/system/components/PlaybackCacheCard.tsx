import { FloppyDisk } from "@phosphor-icons/react";
import { useForm } from "@tanstack/react-form";
import { useTranslation } from "react-i18next";
import type { z } from "zod";
import type { PlaybackCacheConfigResponse } from "@/api/generated/trpc";
import { UpdatePlaybackCacheConfigInputSchema } from "@/api/generated/zod";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import {
	Card,
	CardContent,
	CardDescription,
	CardHeader,
	CardTitle,
} from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Switch } from "@/components/ui/switch";
import { useUpdatePlaybackCacheConfig } from "@/features/system/queries";

type PlaybackCacheFormValues = z.infer<
	typeof UpdatePlaybackCacheConfigInputSchema
>;

function playbackCacheFormValues(
	data: PlaybackCacheConfigResponse,
): PlaybackCacheFormValues {
	return {
		enabled: data.enabled,
		max_percent: data.max_percent,
		auto_generate: data.auto_generate,
	};
}

export function PlaybackCacheCard({
	data,
}: {
	data: PlaybackCacheConfigResponse;
}) {
	const { t } = useTranslation();
	const update = useUpdatePlaybackCacheConfig();

	// Defaults are computed from the loaded config at first render (the parent
	// mounts this card only once config is present), so there's no prop-to-state
	// sync effect. The generated schema (int, 1-100) is the single source of truth
	// for the percentage bound — no hand-rolled clamp; invalid input surfaces as a
	// field error and blocks submit. Successful saves re-baseline from the mutation
	// response.
	const form = useForm({
		defaultValues: playbackCacheFormValues(data),
		validators: {
			onSubmit: UpdatePlaybackCacheConfigInputSchema,
		},
		onSubmit: async ({ value, formApi }) => {
			const saved = await update.mutateAsync(value);
			formApi.reset(playbackCacheFormValues(saved));
		},
	});

	return (
		<Card>
			<CardHeader className="sm:flex-row sm:items-start sm:justify-between">
				<div>
					<CardTitle>{t("playback_cache.card_title")}</CardTitle>
					<CardDescription>
						{t("playback_cache.card_description")}
					</CardDescription>
				</div>
				<form.Subscribe selector={(s) => s.values.enabled}>
					{(enabled) => (
						<Badge variant={enabled ? "green" : "muted"}>
							{enabled
								? t("playback_cache.enabled")
								: t("playback_cache.disabled")}
						</Badge>
					)}
				</form.Subscribe>
			</CardHeader>
			<CardContent>
				<form
					className="grid gap-5"
					onSubmit={(event) => {
						event.preventDefault();
						event.stopPropagation();
						void form.handleSubmit();
					}}
				>
					<form.Field name="enabled">
						{(field) => (
							<div className="flex items-center gap-3">
								<Switch
									id={field.name}
									checked={field.state.value}
									onCheckedChange={(checked) =>
										field.handleChange(checked === true)
									}
									disabled={update.isPending}
								/>
								<Label htmlFor={field.name}>{t("playback_cache.enable")}</Label>
							</div>
						)}
					</form.Field>

					<form.Subscribe selector={(s) => s.values.enabled}>
						{(enabled) => (
							<form.Field name="auto_generate">
								{(field) => (
									<div className="flex items-center gap-3">
										<Switch
											id={field.name}
											checked={field.state.value}
											onCheckedChange={(checked) =>
												field.handleChange(checked === true)
											}
											disabled={update.isPending || !enabled}
										/>
										<Label htmlFor={field.name}>
											{t("playback_cache.auto_generate")}
										</Label>
									</div>
								)}
							</form.Field>
						)}
					</form.Subscribe>

					<form.Field name="max_percent">
						{(field) => (
							<div className="grid gap-1.5 max-w-xs">
								<Label htmlFor={field.name}>
									{t("playback_cache.max_percent")}
								</Label>
								<Input
									id={field.name}
									type="number"
									min={1}
									max={100}
									step={1}
									value={
										Number.isFinite(field.state.value) ? field.state.value : ""
									}
									onChange={(event) =>
										field.handleChange(event.target.valueAsNumber)
									}
									onBlur={field.handleBlur}
									aria-invalid={
										field.state.meta.errors.length > 0 ? true : undefined
									}
									disabled={update.isPending}
								/>
								<span className="text-xs text-muted-foreground">
									{t("playback_cache.max_percent_hint")}
								</span>
								{field.state.meta.errors.length > 0 && (
									<span className="text-xs text-destructive">
										{fieldErrorMessage(field.state.meta.errors)}
									</span>
								)}
							</div>
						)}
					</form.Field>

					{update.isError && (
						<div className="rounded-md bg-destructive/10 border border-destructive/20 p-3 text-destructive text-sm">
							{update.error?.message ?? t("playback_cache.save_failed")}
						</div>
					)}
					{update.isSuccess && (
						<div className="rounded-md bg-primary/10 border border-primary/20 p-3 text-sm">
							{t("playback_cache.saved")}
						</div>
					)}

					<form.Subscribe
						selector={(s) => [s.canSubmit, s.isSubmitting, s.isDirty] as const}
					>
						{([canSubmit, isSubmitting, isDirty]) => (
							<div>
								<Button
									type="submit"
									disabled={
										!canSubmit || isSubmitting || update.isPending || !isDirty
									}
								>
									<FloppyDisk />
									{isSubmitting || update.isPending
										? t("common.saving")
										: t("playback_cache.save")}
								</Button>
							</div>
						)}
					</form.Subscribe>
				</form>
			</CardContent>
		</Card>
	);
}

// TanStack Form surfaces Zod issues as objects with a `message`; fall back to
// the raw value for plain-string errors.
function fieldErrorMessage(errors: readonly unknown[]): string {
	const first = errors[0];
	if (typeof first === "string") return first;
	if (first && typeof first === "object" && "message" in first) {
		return String((first as { message: unknown }).message);
	}
	return "Invalid";
}
