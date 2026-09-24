import {
	ArrowsLeftRightIcon,
	CheckIcon,
	ClockCountdownIcon,
	CopyIcon,
	FloppyDiskIcon,
	GlobeSimpleIcon,
	type Icon,
	ProhibitIcon,
	WarningCircleIcon,
} from "@phosphor-icons/react";
import { useForm } from "@tanstack/react-form";
import type { TFunction } from "i18next";
import { type ReactNode, useEffect, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import type { z } from "zod";
import type {
	ConfigResponse,
	ServerMode,
	UpdateConfigInput,
} from "@/api/generated/trpc";
import { UpdateConfigInputSchema } from "@/api/generated/zod";
import { Alert } from "@/components/ui/alert";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { LoadingState } from "@/components/ui/loading-state";
import { RadioGroup, RadioGroupItem } from "@/components/ui/radio-group";
import {
	BadgeSkeleton,
	ButtonSkeleton,
	Skeleton,
} from "@/components/ui/skeleton";
import { cn } from "@/lib/utils";
import { useUpdateEventSubConfig } from "../queries";

type Mode = ServerMode;

type FormValues = z.infer<typeof UpdateConfigInputSchema>;

const MODE_ORDER: readonly Mode[] = ["direct", "relay", "poll", "off"];

const MODE_OPTION_CLASS =
	"flex items-start gap-3 rounded-lg border p-3.5 transition-colors";

const IDLE_MODE_OPTION_CLASS = "border-border bg-background";

const MODE_ICON: Record<Mode, Icon> = {
	direct: GlobeSimpleIcon,
	relay: ArrowsLeftRightIcon,
	poll: ClockCountdownIcon,
	off: ProhibitIcon,
};

function modeCopy(t: TFunction, mode: Mode): { label: string; desc: string } {
	switch (mode) {
		case "direct":
			return {
				label: t("eventsub.mode_direct"),
				desc: t("eventsub.mode_direct_desc"),
			};
		case "relay":
			return {
				label: t("eventsub.mode_relay"),
				desc: t("eventsub.mode_relay_desc"),
			};
		case "poll":
			return {
				label: t("eventsub.mode_poll"),
				desc: t("eventsub.mode_poll_desc"),
			};
		case "off":
			return {
				label: t("eventsub.mode_off"),
				desc: t("eventsub.mode_off_desc"),
			};
	}
}

function formValues(data: ConfigResponse): FormValues {
	return {
		mode: (data.mode || "poll") as Mode,
		relay_ingest_url: data.relay_ingest_url ?? "",
	};
}

export function payload(values: FormValues): UpdateConfigInput {
	const mode = values.mode as Mode;
	if (mode === "relay") {
		return { mode, relay_ingest_url: values.relay_ingest_url?.trim() ?? "" };
	}
	return { mode };
}

async function copyText(value: string): Promise<boolean> {
	try {
		if (navigator.clipboard?.writeText) {
			await navigator.clipboard.writeText(value);
			return true;
		}
	} catch {}
	try {
		const textarea = document.createElement("textarea");
		textarea.value = value;
		textarea.style.position = "fixed";
		textarea.style.opacity = "0";
		document.body.appendChild(textarea);
		textarea.select();
		const ok = document.execCommand("copy");
		document.body.removeChild(textarea);
		return ok;
	} catch {
		return false;
	}
}

function CopyButton({ value }: { value: string }) {
	const { t } = useTranslation();
	const [copied, setCopied] = useState(false);
	const timer = useRef<ReturnType<typeof setTimeout> | null>(null);

	useEffect(
		() => () => {
			if (timer.current) clearTimeout(timer.current);
		},
		[],
	);

	const onCopy = async () => {
		if (!(await copyText(value))) return;
		setCopied(true);
		if (timer.current) clearTimeout(timer.current);
		timer.current = setTimeout(() => setCopied(false), 1500);
	};

	return (
		<Button
			type="button"
			variant="outline"
			size="sm"
			className="shrink-0"
			onClick={() => void onCopy()}
		>
			{copied ? (
				<CheckIcon data-icon="inline-start" />
			) : (
				<CopyIcon data-icon="inline-start" />
			)}
			{copied ? t("common.copied") : t("common.copy")}
		</Button>
	);
}

function statusVariant(data: ConfigResponse) {
	if (data.setup_required || data.restart_required) return "yellow" as const;
	if (data.mode === "off") return "muted" as const;
	return "green" as const;
}

function ModeOption({
	mode,
	selected,
	disabled,
}: {
	mode: Mode;
	selected: boolean;
	disabled: boolean;
}) {
	return (
		// biome-ignore lint/a11y/noLabelWithoutControl: the control is the nested Base UI radio (RadioGroupItem); biome can't see it statically.
		<label
			data-dimmed={disabled || undefined}
			className={cn(
				MODE_OPTION_CLASS,
				disabled ? "cursor-not-allowed opacity-60" : "cursor-pointer",
				selected
					? "border-accent bg-accent"
					: cn(
							IDLE_MODE_OPTION_CLASS,
							"hover:border-ring-muted hover:bg-accent/40",
						),
			)}
		>
			<ModeOptionBody
				mode={mode}
				selected={selected}
				control={<RadioGroupItem value={mode} className="mt-0.5 shrink-0" />}
			/>
		</label>
	);
}

function ModeOptionBody({
	mode,
	selected,
	control,
}: {
	mode: Mode;
	selected: boolean;
	control: ReactNode;
}) {
	const { t } = useTranslation();
	const ModeGlyph = MODE_ICON[mode];
	const { label, desc } = modeCopy(t, mode);

	return (
		<>
			<span
				className={cn(
					"flex size-9 shrink-0 items-center justify-center rounded-md transition-colors",
					selected
						? "bg-primary/15 text-primary"
						: "bg-muted text-muted-foreground",
				)}
			>
				<ModeGlyph className="size-5" />
			</span>
			<div className="min-w-0 flex-1">
				<div className="flex items-center justify-between gap-2">
					<span className="text-sm font-medium text-foreground">{label}</span>
					{control}
				</div>
				<p className="mt-1 text-xs leading-relaxed text-muted-foreground">
					{desc}
				</p>
			</div>
		</>
	);
}

function UrlField({
	id,
	label,
	value,
	placeholder,
	hint,
	disabled,
	onChange,
	className,
}: {
	id: string;
	label: string;
	value: string;
	placeholder: string;
	hint?: string;
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
			{hint ? <p className="text-xs text-muted-foreground">{hint}</p> : null}
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
				formApi.reset(formValues(saved));
			} catch {}
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

	const disabled = data.env_managed || update.isPending;
	const restartRequired = data.restart_required;

	return (
		<Card>
			<CardHeader className="sm:flex-row sm:items-start sm:justify-between">
				<div>
					<CardTitle className="flex items-center gap-2">
						{data.setup_required ? (
							<WarningCircleIcon className="size-5 text-yellow-600" />
						) : null}
						{t("eventsub.config_title")}
					</CardTitle>
				</div>
				{!data.env_managed && (
					<Badge variant={statusVariant(data)}>
						{data.restart_required
							? t("eventsub.restart_required")
							: data.setup_required
								? t("eventsub.setup_required")
								: t("eventsub.active")}
					</Badge>
				)}
			</CardHeader>
			<CardContent>
				{data.env_managed ? (
					<div className="rounded-md border border-border bg-muted/40 p-3 text-sm text-foreground">
						{t("eventsub.env_managed_hint")}
					</div>
				) : (
					<form
						className="grid gap-5"
						onSubmit={(event) => {
							event.preventDefault();
							event.stopPropagation();
							void form.handleSubmit();
						}}
					>
						<div className="grid gap-2.5">
							<Label>{t("eventsub.choose_mode")}</Label>
							<form.Field name="mode">
								{(field) => (
									<RadioGroup
										aria-label={t("eventsub.choose_mode")}
										value={field.state.value}
										onValueChange={(value) => field.handleChange(value as Mode)}
										disabled={disabled}
										className="grid gap-3 sm:grid-cols-2"
									>
										{MODE_ORDER.map((mode) => (
											<ModeOption
												key={mode}
												mode={mode}
												selected={field.state.value === mode}
												disabled={disabled}
											/>
										))}
									</RadioGroup>
								)}
							</form.Field>
						</div>

						<form.Subscribe selector={(s) => s.values.mode}>
							{(mode) => (
								<>
									{mode === "direct" &&
										(data.direct_callback_url ? (
											<div className="grid gap-1.5">
												<Label>{t("eventsub.webhook_callback_url")}</Label>
												<div className="flex items-center gap-2">
													<code className="min-w-0 flex-1 truncate rounded-md border border-border bg-muted/40 px-3 py-2 font-mono text-xs text-foreground">
														{data.direct_callback_url}
													</code>
													<CopyButton value={data.direct_callback_url} />
												</div>
												<p className="text-xs text-muted-foreground">
													{t("eventsub.direct_callback_hint")}
												</p>
											</div>
										) : (
											<div className="rounded-md border border-yellow-500/30 bg-yellow-500/10 p-3 text-sm text-foreground">
												{t("eventsub.direct_needs_public_url")}
											</div>
										))}

									{mode === "relay" && (
										<form.Field name="relay_ingest_url">
											{(field) => (
												<UrlField
													id="eventsub-relay"
													label={t("eventsub.relay_url")}
													value={field.state.value ?? ""}
													placeholder="https://relay.replayvod.com/u/your-token"
													hint={t("eventsub.relay_url_hint")}
													disabled={disabled}
													onChange={(value) => field.handleChange(value)}
												/>
											)}
										</form.Field>
									)}
								</>
							)}
						</form.Subscribe>

						{update.isError && (
							<Alert variant="destructive">
								{update.error?.message ?? t("eventsub.config_save_failed")}
							</Alert>
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
									<Alert variant="success">{t("eventsub.config_saved")}</Alert>
								) : null
							}
						</form.Subscribe>

						<div>
							<form.Subscribe selector={(s) => s.values.mode}>
								{(mode) => (
									<Button
										type="submit"
										disabled={
											disabled ||
											(mode === "direct" && !data.direct_callback_url)
										}
									>
										<FloppyDiskIcon data-icon="inline-start" />
										{update.isPending
											? t("common.saving")
											: t("eventsub.save_config")}
									</Button>
								)}
							</form.Subscribe>
						</div>
					</form>
				)}
			</CardContent>
		</Card>
	);
}

export function EventSubSetupCardSkeleton() {
	const { t } = useTranslation();
	return (
		<LoadingState>
			<Card>
				<CardHeader className="sm:flex-row sm:items-start sm:justify-between">
					<div>
						<CardTitle className="flex items-center gap-2">
							{t("eventsub.config_title")}
						</CardTitle>
					</div>
					<BadgeSkeleton />
				</CardHeader>
				<CardContent>
					<div className="grid gap-5">
						<div className="grid gap-2.5">
							<Label>{t("eventsub.choose_mode")}</Label>
							<div className="grid gap-3 sm:grid-cols-2">
								{MODE_ORDER.map((mode) => (
									<div
										key={mode}
										className={cn(MODE_OPTION_CLASS, IDLE_MODE_OPTION_CLASS)}
									>
										<ModeOptionBody
											mode={mode}
											selected={false}
											control={
												<Skeleton className="mt-0.5 size-4 shrink-0 rounded-full" />
											}
										/>
									</div>
								))}
							</div>
						</div>
						<div>
							<ButtonSkeleton className="w-36" />
						</div>
					</div>
				</CardContent>
			</Card>
		</LoadingState>
	);
}
