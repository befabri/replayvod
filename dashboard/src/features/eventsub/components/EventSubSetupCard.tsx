import { CheckCircle, FloppyDisk, WarningCircle } from "@phosphor-icons/react";
import { useEffect, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import type { ConfigResponse, UpdateConfigInput } from "@/api/generated/trpc";
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

type Mode = "off" | "poll" | "direct" | "relay";

const defaultValues: UpdateConfigInput = {
	mode: "poll",
	webhook_callback_url: "",
	relay_ingest_url: "",
	relay_subscribe_url: "",
	relay_local_callback_url: "",
};

function formValues(data: ConfigResponse): UpdateConfigInput {
	return {
		mode: data.mode || "poll",
		webhook_callback_url: data.webhook_callback_url ?? "",
		relay_ingest_url: data.relay_ingest_url ?? "",
		relay_subscribe_url: data.relay_subscribe_url ?? "",
		relay_local_callback_url: data.relay_local_callback_url ?? "",
	};
}

function sameFormValues(a: UpdateConfigInput, b: UpdateConfigInput): boolean {
	return (
		(a.mode ?? "") === (b.mode ?? "") &&
		(a.webhook_callback_url ?? "") === (b.webhook_callback_url ?? "") &&
		(a.relay_ingest_url ?? "") === (b.relay_ingest_url ?? "") &&
		(a.relay_subscribe_url ?? "") === (b.relay_subscribe_url ?? "") &&
		(a.relay_local_callback_url ?? "") === (b.relay_local_callback_url ?? "")
	);
}

export function formValuesAfterConfigRefresh(
	current: UpdateConfigInput,
	previousBaseline: UpdateConfigInput,
	nextBaseline: UpdateConfigInput,
): UpdateConfigInput {
	return sameFormValues(current, previousBaseline) ? nextBaseline : current;
}

export function formValuesDirty(
	current: UpdateConfigInput,
	baseline: UpdateConfigInput,
): boolean {
	return !sameFormValues(current, baseline);
}

// payload reduces the form to only the fields the chosen mode uses,
// trimmed. It mirrors the server's ClearURLsForDelivery so we never send (and
// the server never has to clear) URLs foreign to the mode. Exported for tests.
export function payload(values: UpdateConfigInput): UpdateConfigInput {
	const mode = values.mode as Mode;
	if (mode === "off" || mode === "poll") {
		return { ...defaultValues, mode };
	}
	if (mode === "direct") {
		return {
			...defaultValues,
			mode,
			webhook_callback_url: values.webhook_callback_url?.trim() ?? "",
		};
	}
	return {
		...defaultValues,
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
	const [values, setValues] = useState<UpdateConfigInput>(() =>
		formValues(data),
	);
	const [savedResponse, setSavedResponse] = useState<ConfigResponse | null>(
		null,
	);
	const baselineRef = useRef<UpdateConfigInput>(formValues(data));

	useEffect(() => {
		const next = formValues(data);
		const previousBaseline = baselineRef.current;
		setValues((current) =>
			formValuesAfterConfigRefresh(current, previousBaseline, next),
		);
		baselineRef.current = next;
		setSavedResponse(null);
	}, [data]);

	const setField = (key: keyof UpdateConfigInput, value: string) =>
		setValues((current) => ({ ...current, [key]: value }));

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

	const mode = (values.mode || "poll") as Mode;
	const latest = savedResponse ?? data;
	const disabled = latest.env_managed || update.isPending;
	const restartRequired = latest.restart_required;
	const dirty = formValuesDirty(values, baselineRef.current);

	return (
		<Card>
			<CardHeader className="sm:flex-row sm:items-start sm:justify-between">
				<div>
					<CardTitle className="flex items-center gap-2">
						{latest.setup_required ? (
							<WarningCircle className="size-5 text-yellow-600" />
						) : (
							<CheckCircle className="size-5 text-green-600" />
						)}
						{t("eventsub.config_title")}
					</CardTitle>
					<CardDescription>{t("eventsub.config_description")}</CardDescription>
				</div>
				<Badge variant={statusVariant(latest)}>
					{latest.restart_required
						? t("eventsub.restart_required")
						: latest.setup_required
							? t("eventsub.setup_required")
							: latest.env_managed
								? t("eventsub.env_managed")
								: t("eventsub.active")}
				</Badge>
			</CardHeader>
			<CardContent>
				{latest.env_managed ? (
					<div className="rounded-md border border-border bg-muted/40 p-3 text-sm text-foreground">
						{t("eventsub.env_managed_hint")}
					</div>
				) : (
					<form
						className="grid gap-4"
						onSubmit={(event) => {
							event.preventDefault();
							update.mutate(payload(values), {
								onSuccess: (saved) => {
									const next = formValues(saved);
									baselineRef.current = next;
									setValues(next);
									setSavedResponse(saved);
								},
							});
						}}
					>
						<div className="grid gap-1.5 sm:max-w-xs">
							<Label htmlFor="eventsub-mode">{t("eventsub.mode")}</Label>
							<Select
								value={mode}
								onValueChange={(value) => setField("mode", value as string)}
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
									<SelectItem value="off">{t("eventsub.mode_off")}</SelectItem>
								</SelectContent>
							</Select>
						</div>

						{mode === "direct" && (
							<UrlField
								id="eventsub-webhook"
								label={t("eventsub.webhook_callback_url")}
								value={values.webhook_callback_url ?? ""}
								placeholder="https://replayvod.example/api/v1/webhook/callback"
								disabled={disabled}
								onChange={(value) => setField("webhook_callback_url", value)}
							/>
						)}

						{mode === "relay" && (
							<div className="grid gap-4 lg:grid-cols-2">
								<UrlField
									id="eventsub-relay-ingest"
									label={t("eventsub.relay_ingest_url")}
									value={values.relay_ingest_url ?? ""}
									placeholder="https://relay.replayvod.com/u/token"
									disabled={disabled}
									onChange={(value) => setField("relay_ingest_url", value)}
								/>
								<UrlField
									id="eventsub-relay-subscribe"
									label={t("eventsub.relay_subscribe_url")}
									value={values.relay_subscribe_url ?? ""}
									placeholder="wss://relay.replayvod.com/u/token/subscribe"
									disabled={disabled}
									onChange={(value) => setField("relay_subscribe_url", value)}
								/>
								<UrlField
									id="eventsub-relay-local"
									label={t("eventsub.relay_local_callback_url")}
									value={values.relay_local_callback_url ?? ""}
									placeholder="http://127.0.0.1:8080/api/v1/webhook/callback"
									disabled={disabled}
									onChange={(value) =>
										setField("relay_local_callback_url", value)
									}
									className="lg:col-span-2"
								/>
							</div>
						)}

						{update.isError && (
							<div className="rounded-md border border-destructive/20 bg-destructive/10 p-3 text-sm text-destructive">
								{update.error?.message ?? t("eventsub.config_save_failed")}
							</div>
						)}

						{restartRequired && (
							<div className="rounded-md border border-yellow-500/30 bg-yellow-500/10 p-3 text-sm text-foreground">
								{t("eventsub.restart_hint")}{" "}
								{t("eventsub.currently_running", {
									mode: modeLabel(latest.active?.mode ?? ""),
								})}
							</div>
						)}

						{update.isSuccess && !dirty && !restartRequired && (
							<div className="rounded-md border border-primary/20 bg-primary/10 p-3 text-sm text-foreground">
								{t("eventsub.config_saved")}
							</div>
						)}

						<div>
							<Button type="submit" disabled={disabled}>
								<FloppyDisk data-icon="inline-start" />
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
