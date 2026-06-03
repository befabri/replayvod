import {
	ArrowsClockwiseIcon,
	CopyIcon,
	EyeIcon,
	EyeSlashIcon,
	FloppyDiskIcon,
	PaperPlaneTiltIcon,
} from "@phosphor-icons/react";
import { useForm } from "@tanstack/react-form";
import { useState } from "react";
import { useTranslation } from "react-i18next";
import { toast } from "sonner";
import type {
	RecordingWebhookConfigResponse as ConfigResponse,
	RecordingWebhookUpdateConfigInput as UpdateConfigInput,
} from "@/api/generated/trpc";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import {
	Card,
	CardContent,
	CardDescription,
	CardHeader,
	CardTitle,
} from "@/components/ui/card";
import { Checkbox } from "@/components/ui/checkbox";
import {
	Dialog,
	DialogContent,
	DialogDescription,
	DialogFooter,
	DialogHeader,
	DialogTitle,
} from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Switch } from "@/components/ui/switch";
import {
	useRegenerateRecordingWebhookSecret,
	useTestRecordingWebhookDelivery,
	useUpdateRecordingWebhookConfig,
} from "../queries";
import {
	RecordingWebhookFormSchema,
	type RecordingWebhookFormValues,
} from "../schema";

const EVENT_COMPLETED = "recording.completed";
const EVENT_FAILED = "recording.failed";

// formStateFromConfig maps a loaded config to form values. An empty events list
// means "all events", which presents as both boxes checked. The signing secret
// is not editable: it is displayed read-only from the loaded config and rotated
// via the Regenerate button, so it is not part of the form.
export function formStateFromConfig(
	data: ConfigResponse,
): RecordingWebhookFormValues {
	const events = data.events ?? [];
	const all = events.length === 0;
	return {
		enabled: data.enabled,
		url: data.url ?? "",
		onCompleted: all || events.includes(EVENT_COMPLETED),
		onFailed: all || events.includes(EVENT_FAILED),
	};
}

// buildPayload reduces the form to the update input. Events carries only the
// checked identifiers; the URL is trimmed. The secret is never sent: it is
// managed server-side and rotated through its own action.
export function buildPayload(
	state: RecordingWebhookFormValues,
): UpdateConfigInput {
	const events: string[] = [];
	if (state.onCompleted) events.push(EVENT_COMPLETED);
	if (state.onFailed) events.push(EVENT_FAILED);
	return {
		enabled: state.enabled,
		url: state.url.trim(),
		events,
	};
}

export function RecordingWebhookCard({ data }: { data: ConfigResponse }) {
	const { t } = useTranslation();
	const update = useUpdateRecordingWebhookConfig();
	const regenerate = useRegenerateRecordingWebhookSecret();
	const test = useTestRecordingWebhookDelivery();

	// Pure UI state, not form data: the secret reveal toggle and the rotate
	// confirmation dialog. Form values live in the form store below.
	const [showSecret, setShowSecret] = useState(false);
	const [confirmRotate, setConfirmRotate] = useState(false);

	const form = useForm({
		defaultValues: formStateFromConfig(data),
		validators: {
			onChange: RecordingWebhookFormSchema,
			onSubmit: RecordingWebhookFormSchema,
		},
		onSubmit: async ({ value, formApi }) => {
			try {
				await update.mutateAsync(buildPayload(value));
				// Adopt the just-saved values as the new clean baseline so the form
				// reads as saved and "Send test" re-enables. The refetch the mutation
				// triggers refreshes the read-only secret rendered from `data`.
				formApi.reset(value);
			} catch {
				// A failed save is surfaced through the update.isError banner below;
				// swallow the rejection (mutateAsync rethrows) so it does not become
				// an unhandled rejection, and leave the form dirty for a retry.
			}
		},
	});

	const busy = update.isPending || regenerate.isPending || test.isPending;

	const copySecret = async () => {
		try {
			await navigator.clipboard.writeText(data.secret);
			toast.success(t("webhook.secret_copied"));
		} catch {
			toast.error(t("webhook.secret_copy_failed"));
		}
	};

	const doRotate = () => {
		setConfirmRotate(false);
		regenerate.mutate(undefined, {
			onSuccess: () => toast.success(t("webhook.secret_rotated")),
		});
	};

	return (
		<Card>
			<CardHeader className="sm:flex-row sm:items-start sm:justify-between">
				<div>
					<CardTitle>{t("webhook.title")}</CardTitle>
					<CardDescription>{t("webhook.description")}</CardDescription>
				</div>
				<Badge variant={data.enabled ? "green" : "muted"}>
					{data.enabled ? t("webhook.enabled") : t("webhook.disabled")}
				</Badge>
			</CardHeader>
			<CardContent>
				<form
					className="grid gap-4"
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
									id="webhook-enabled"
									checked={field.state.value}
									onCheckedChange={(checked) =>
										field.handleChange(checked === true)
									}
									disabled={busy}
								/>
								<Label htmlFor="webhook-enabled">{t("webhook.enable")}</Label>
							</div>
						)}
					</form.Field>

					<form.Field name="url">
						{(field) => (
							<div className="grid gap-1.5">
								<Label htmlFor="webhook-url">{t("webhook.url")}</Label>
								<Input
									id="webhook-url"
									type="url"
									value={field.state.value}
									onChange={(event) => field.handleChange(event.target.value)}
									onBlur={field.handleBlur}
									placeholder="https://hooks.example.com/replayvod"
									disabled={busy}
								/>
								<span className="text-xs text-muted-foreground">
									{t("webhook.url_hint")}
								</span>
							</div>
						)}
					</form.Field>

					<fieldset className="grid gap-2">
						<legend className="text-sm font-medium">
							{t("webhook.events")}
						</legend>
						<form.Field name="onCompleted">
							{(field) => (
								<div className="flex items-center gap-2">
									<Checkbox
										id="webhook-event-completed"
										checked={field.state.value}
										onCheckedChange={(c) => field.handleChange(c === true)}
										disabled={busy}
									/>
									<Label htmlFor="webhook-event-completed">
										{t("webhook.event_completed")}
									</Label>
								</div>
							)}
						</form.Field>
						<form.Field name="onFailed">
							{(field) => (
								<div className="flex items-center gap-2">
									<Checkbox
										id="webhook-event-failed"
										checked={field.state.value}
										onCheckedChange={(c) => field.handleChange(c === true)}
										disabled={busy}
									/>
									<Label htmlFor="webhook-event-failed">
										{t("webhook.event_failed")}
									</Label>
								</div>
							)}
						</form.Field>
						<form.Subscribe
							selector={(s) => !s.values.onCompleted && !s.values.onFailed}
						>
							{(blocked) =>
								blocked ? (
									<span className="text-xs text-destructive">
										{t("webhook.events_required")}
									</span>
								) : null
							}
						</form.Subscribe>
					</fieldset>

					{data.secret && (
						<div className="grid gap-1.5">
							<Label htmlFor="webhook-secret">{t("webhook.secret")}</Label>
							<div className="flex items-center gap-2">
								<Input
									id="webhook-secret"
									type={showSecret ? "text" : "password"}
									value={data.secret}
									readOnly
									className="font-mono"
								/>
								<Button
									type="button"
									variant="outline"
									size="icon"
									aria-label={
										showSecret
											? t("webhook.hide_secret")
											: t("webhook.show_secret")
									}
									onClick={() => setShowSecret((v) => !v)}
								>
									{showSecret ? <EyeSlashIcon /> : <EyeIcon />}
								</Button>
								<Button
									type="button"
									variant="outline"
									size="icon"
									aria-label={t("webhook.copy_secret")}
									onClick={copySecret}
								>
									<CopyIcon />
								</Button>
							</div>
							<span className="text-xs text-muted-foreground">
								{t("webhook.secret_hint")}
							</span>
						</div>
					)}

					{update.isError && (
						<div className="rounded-md border border-destructive/20 bg-destructive/10 p-3 text-sm text-destructive">
							{update.error?.message ?? t("webhook.save_failed")}
						</div>
					)}

					{update.isSuccess && (
						<div className="rounded-md border border-primary/20 bg-primary/10 p-3 text-sm text-foreground">
							{t("webhook.saved")}
						</div>
					)}

					{test.isSuccess && <TestResultBanner result={test.data} />}

					<form.Subscribe
						selector={(s) => [s.canSubmit, s.isSubmitting, s.isDirty] as const}
					>
						{([canSubmit, isSubmitting, isDirty]) => (
							<div className="flex flex-wrap gap-2">
								<Button
									type="submit"
									disabled={busy || !canSubmit || isSubmitting}
								>
									<FloppyDiskIcon data-icon="inline-start" />
									{isSubmitting || update.isPending
										? t("common.saving")
										: t("webhook.save")}
								</Button>
								{data.url && (
									<Button
										type="button"
										variant="outline"
										disabled={busy || isDirty}
										onClick={() => test.mutate()}
									>
										<PaperPlaneTiltIcon data-icon="inline-start" />
										{test.isPending
											? t("webhook.testing")
											: t("webhook.send_test")}
									</Button>
								)}
								{data.secret && (
									<Button
										type="button"
										variant="outline"
										disabled={busy}
										onClick={() => setConfirmRotate(true)}
									>
										<ArrowsClockwiseIcon data-icon="inline-start" />
										{t("webhook.regenerate_secret")}
									</Button>
								)}
							</div>
						)}
					</form.Subscribe>
				</form>
			</CardContent>

			<Dialog open={confirmRotate} onOpenChange={setConfirmRotate}>
				<DialogContent>
					<DialogHeader>
						<DialogTitle>{t("webhook.rotate_confirm_title")}</DialogTitle>
						<DialogDescription>
							{t("webhook.rotate_confirm_body")}
						</DialogDescription>
					</DialogHeader>
					<DialogFooter>
						<Button
							type="button"
							variant="outline"
							onClick={() => setConfirmRotate(false)}
							disabled={regenerate.isPending}
						>
							{t("common.cancel")}
						</Button>
						<Button
							type="button"
							onClick={doRotate}
							disabled={regenerate.isPending}
						>
							{t("webhook.regenerate_secret")}
						</Button>
					</DialogFooter>
				</DialogContent>
			</Dialog>
		</Card>
	);
}

// TestResultBanner renders the synchronous outcome of a test delivery.
function TestResultBanner({
	result,
}: {
	result: { ok: boolean; status: number; error?: string };
}) {
	const { t } = useTranslation();
	if (result.ok) {
		return (
			<div className="rounded-md border border-primary/20 bg-primary/10 p-3 text-sm text-foreground">
				{t("webhook.test_ok", { status: result.status })}
			</div>
		);
	}
	return (
		<div className="rounded-md border border-destructive/20 bg-destructive/10 p-3 text-sm text-destructive">
			{result.error
				? t("webhook.test_failed_reason", { reason: result.error })
				: t("webhook.test_failed", { status: result.status })}
		</div>
	);
}
