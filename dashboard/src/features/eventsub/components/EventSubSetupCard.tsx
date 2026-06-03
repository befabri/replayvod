import {
	CheckCircleIcon,
	FloppyDiskIcon,
	WarningCircleIcon,
} from "@phosphor-icons/react";
import { useForm } from "@tanstack/react-form";
import { useTranslation } from "react-i18next";
import type { z } from "zod";
import type {
	ConfigResponse,
	ServerMode,
	UpdateConfigInput,
} from "@/api/generated/trpc";
import { UpdateConfigInputSchema } from "@/api/generated/zod";
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
import {
	Select,
	SelectContent,
	SelectItem,
	SelectTrigger,
	SelectValue,
} from "@/components/ui/select";
import { cn } from "@/lib/utils";
import { useUpdateEventSubConfig } from "../queries";

// Mode is the generated ServerMode union (Go config.ServerMode); the form's
// FormValues["mode"] resolves to the same set via the oneof on the input schema.
type Mode = ServerMode;

// FormValues is the validated config shape; mode is narrowed to the enum the
// schema enforces (the wire type widens it to string).
type FormValues = z.infer<typeof UpdateConfigInputSchema>;

// blankConfig is the all-URLs-cleared baseline payload reduces a mode to.
const blankConfig: FormValues = {
	mode: "poll",
	webhook_callback_url: "",
	relay_ingest_url: "",
	relay_subscribe_url: "",
	relay_local_callback_url: "",
};

function formValues(data: ConfigResponse): FormValues {
	return {
		mode: (data.mode || "poll") as Mode,
		webhook_callback_url: data.webhook_callback_url ?? "",
		relay_ingest_url: data.relay_ingest_url ?? "",
		relay_subscribe_url: data.relay_subscribe_url ?? "",
		relay_local_callback_url: data.relay_local_callback_url ?? "",
	};
}

// payload reduces the form to only the fields the chosen mode uses,
// trimmed. It mirrors the server's ClearURLsForDelivery so we never send (and
// the server never has to clear) URLs foreign to the mode. Exported for tests.
export function payload(values: FormValues): UpdateConfigInput {
	const mode = values.mode as Mode;
	if (mode === "off" || mode === "poll") {
		return { ...blankConfig, mode };
	}
	if (mode === "direct") {
		return {
			...blankConfig,
			mode,
			webhook_callback_url: values.webhook_callback_url?.trim() ?? "",
		};
	}
	return {
		...blankConfig,
		mode,
		relay_ingest_url: values.relay_ingest_url?.trim() ?? "",
		relay_subscribe_url: values.relay_subscribe_url?.trim() ?? "",
		relay_local_callback_url: values.relay_local_callback_url?.trim() ?? "",
	};
}

function statusVariant(data: ConfigResponse) {
	if (data.setup_required || data.restart_required) return "yellow" as const;
	if (data.env_managed) return "blue" as const;
	if (data.mode === "off") return "muted" as const;
	return "green" as const;
}

// UrlField is the single controlled input every URL-backed mode reuses. Folding
// the per-field Label+Input+onChange into one component removes the copy-paste
// where a field could write to the wrong state key.
function UrlField({
	id,
	label,
	value,
	placeholder,
	disabled,
	onChange,
	className,
}: {
	id: string;
	label: string;
	value: string;
	placeholder: string;
	disabled: boolean;
	onChange: (value: string) => void;
	className?: string;
}) {
	return (
		<div className={cn("grid gap-1.5", className)}>
			<Label htmlFor={id}>{label}</Label>
			<Input
				id={id}
				type="url"
				value={value}
				onChange={(event) => onChange(event.target.value)}
				placeholder={placeholder}
				disabled={disabled}
			/>
		</div>
	);
}

export function EventSubSetupCard({ data }: { data: ConfigResponse }) {
	const { t } = useTranslation();
	const update = useUpdateEventSubConfig();

	const form = useForm({
		defaultValues: formValues(data),
		validators: {
			onSubmit: UpdateConfigInputSchema,
		},
		onSubmit: async ({ value, formApi }) => {
			try {
				const saved = await update.mutateAsync(payload(value));
				// Adopt the saved config as the new clean baseline. mutateAsync resolves
				// with the fresh config, so the form goes clean and the "saved" banner
				// shows without waiting for the invalidation refetch to land.
				formApi.reset(formValues(saved));
			} catch {
				// A failed save is surfaced through the update.isError banner below;
				// swallow the rejection (mutateAsync rethrows) so it does not become
				// an unhandled rejection, and leave the form dirty for a retry.
			}
		},
	});

	const modeLabel = (value: string) => {
		switch (value) {
			case "direct":
				return t("eventsub.mode_direct");
			case "relay":
				return t("eventsub.mode_relay");
			case "poll":
				return t("eventsub.mode_poll");
			case "off":
				return t("eventsub.mode_off");
			default:
				return t("eventsub.mode_none");
		}
	};

	// The persistent server state (restart/setup/env-managed/active mode) is read
	// from the fresh query `data`, never from the mutation response: a sticky
	// `update.data` would keep showing the pre-save snapshot after the owner
	// restarts and the config query refetches (e.g. on window focus), leaving the
	// "restart required" banner stuck. The transient "saved" banner keys off
	// update.isSuccess instead, so it does not need the response either.
	const disabled = data.env_managed || update.isPending;
	const restartRequired = data.restart_required;

	return (
		<Card>
			<CardHeader className="sm:flex-row sm:items-start sm:justify-between">
				<div>
					<CardTitle className="flex items-center gap-2">
						{data.setup_required ? (
							<WarningCircleIcon className="size-5 text-yellow-600" />
						) : (
							<CheckCircleIcon className="size-5 text-green-600" />
						)}
						{t("eventsub.config_title")}
					</CardTitle>
					<CardDescription>{t("eventsub.config_description")}</CardDescription>
				</div>
				<Badge variant={statusVariant(data)}>
					{data.restart_required
						? t("eventsub.restart_required")
						: data.setup_required
							? t("eventsub.setup_required")
							: data.env_managed
								? t("eventsub.env_managed")
								: t("eventsub.active")}
				</Badge>
			</CardHeader>
			<CardContent>
				{data.env_managed ? (
					<div className="rounded-md border border-border bg-muted/40 p-3 text-sm text-foreground">
						{t("eventsub.env_managed_hint")}
					</div>
				) : (
					<form
						className="grid gap-4"
						onSubmit={(event) => {
							event.preventDefault();
							event.stopPropagation();
							void form.handleSubmit();
						}}
					>
						<div className="grid gap-1.5 sm:max-w-xs">
							<Label htmlFor="eventsub-mode">{t("eventsub.mode")}</Label>
							<form.Field name="mode">
								{(field) => (
									<Select
										value={field.state.value}
										onValueChange={(value) => field.handleChange(value as Mode)}
										disabled={disabled}
									>
										<SelectTrigger id="eventsub-mode">
											<SelectValue />
										</SelectTrigger>
										<SelectContent>
											<SelectItem value="poll">
												{t("eventsub.mode_poll")}
											</SelectItem>
											<SelectItem value="relay">
												{t("eventsub.mode_relay")}
											</SelectItem>
											<SelectItem value="direct">
												{t("eventsub.mode_direct")}
											</SelectItem>
											<SelectItem value="off">
												{t("eventsub.mode_off")}
											</SelectItem>
										</SelectContent>
									</Select>
								)}
							</form.Field>
						</div>

						<form.Subscribe selector={(s) => s.values.mode}>
							{(mode) => (
								<>
									{mode === "direct" && (
										<form.Field name="webhook_callback_url">
											{(field) => (
												<UrlField
													id="eventsub-webhook"
													label={t("eventsub.webhook_callback_url")}
													value={field.state.value ?? ""}
													placeholder="https://replayvod.example/api/v1/webhook/callback"
													disabled={disabled}
													onChange={(value) => field.handleChange(value)}
												/>
											)}
										</form.Field>
									)}

									{mode === "relay" && (
										<div className="grid gap-4 lg:grid-cols-2">
											<form.Field name="relay_ingest_url">
												{(field) => (
													<UrlField
														id="eventsub-relay-ingest"
														label={t("eventsub.relay_ingest_url")}
														value={field.state.value ?? ""}
														placeholder="https://relay.replayvod.com/u/token"
														disabled={disabled}
														onChange={(value) => field.handleChange(value)}
													/>
												)}
											</form.Field>
											<form.Field name="relay_subscribe_url">
												{(field) => (
													<UrlField
														id="eventsub-relay-subscribe"
														label={t("eventsub.relay_subscribe_url")}
														value={field.state.value ?? ""}
														placeholder="wss://relay.replayvod.com/u/token/subscribe"
														disabled={disabled}
														onChange={(value) => field.handleChange(value)}
													/>
												)}
											</form.Field>
											<form.Field name="relay_local_callback_url">
												{(field) => (
													<UrlField
														id="eventsub-relay-local"
														label={t("eventsub.relay_local_callback_url")}
														value={field.state.value ?? ""}
														placeholder="http://127.0.0.1:8080/api/v1/webhook/callback"
														disabled={disabled}
														onChange={(value) => field.handleChange(value)}
														className="lg:col-span-2"
													/>
												)}
											</form.Field>
										</div>
									)}
								</>
							)}
						</form.Subscribe>

						{update.isError && (
							<div className="rounded-md border border-destructive/20 bg-destructive/10 p-3 text-sm text-destructive">
								{update.error?.message ?? t("eventsub.config_save_failed")}
							</div>
						)}

						{restartRequired && (
							<div className="rounded-md border border-yellow-500/30 bg-yellow-500/10 p-3 text-sm text-foreground">
								{t("eventsub.restart_hint")}{" "}
								{t("eventsub.currently_running", {
									mode: modeLabel(data.active?.mode ?? ""),
								})}
							</div>
						)}

						<form.Subscribe selector={(s) => s.isDirty}>
							{(dirty) =>
								update.isSuccess && !dirty && !restartRequired ? (
									<div className="rounded-md border border-primary/20 bg-primary/10 p-3 text-sm text-foreground">
										{t("eventsub.config_saved")}
									</div>
								) : null
							}
						</form.Subscribe>

						<div>
							<Button type="submit" disabled={disabled}>
								<FloppyDiskIcon data-icon="inline-start" />
								{update.isPending
									? t("common.saving")
									: t("eventsub.save_config")}
							</Button>
						</div>
					</form>
				)}
			</CardContent>
		</Card>
	);
}
