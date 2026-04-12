import { useForm } from "@tanstack/react-form";
import { Info } from "@phosphor-icons/react";
import { useState } from "react";
import { useTranslation } from "react-i18next";
import { z } from "zod";

import { Button } from "@/components/ui/button";
import { Checkbox } from "@/components/ui/checkbox";
import {
	Dialog,
	DialogContent,
	DialogDescription,
	DialogFooter,
	DialogHeader,
	DialogTitle,
	DialogTrigger,
} from "@/components/ui/dialog";
import { Label } from "@/components/ui/label";
import { RadioGroup, RadioGroupItem } from "@/components/ui/radio-group";
import {
	Select,
	SelectContent,
	SelectItem,
	SelectTrigger,
	SelectValue,
} from "@/components/ui/select";
import {
	Tooltip,
	TooltipContent,
	TooltipProvider,
	TooltipTrigger,
} from "@/components/ui/tooltip";
import { useTriggerDownload } from "@/features/videos/queries";

// TriggerDownloadFormSchema is a client-side narrowing of the tRPC
// TriggerDownloadInputSchema: the generated schema accepts empty strings
// (`z.literal("")`) for backward compat, but the dialog only ever
// produces the enum values, so we tighten here. broadcaster_id is
// supplied by props, not the form, and lives outside the schema.
const TriggerDownloadFormSchema = z.object({
	recording_type: z.enum(["video", "audio"]),
	quality: z.enum(["LOW", "MEDIUM", "HIGH"]),
	force_h264: z.boolean(),
});

type TriggerDownloadFormValues = z.infer<typeof TriggerDownloadFormSchema>;

// TriggerDownloadDialog wraps its children with a dialog trigger; the
// form inside submits video.triggerDownload with the operator's mode +
// codec choices. Backend wires recording_type + force_h264 onto the
// videos row; the HLS pipeline will consume them once the native
// downloader lands. See .docs/spec/download-pipeline.md "Recording
// modes" + "UI surface".
export function TriggerDownloadDialog({
	broadcasterId,
	broadcasterName,
	children,
}: {
	broadcasterId: string;
	broadcasterName: string;
	children: React.ReactNode;
}) {
	const { t } = useTranslation();
	const [open, setOpen] = useState(false);
	const trigger = useTriggerDownload();

	const form = useForm({
		defaultValues: {
			recording_type: "video",
			quality: "HIGH",
			force_h264: false,
		} as TriggerDownloadFormValues,
		validators: {
			onSubmit: TriggerDownloadFormSchema,
		},
		onSubmit: async ({ value, formApi }) => {
			const isAudio = value.recording_type === "audio";
			await trigger.mutateAsync({
				broadcaster_id: broadcasterId,
				recording_type: value.recording_type,
				// Audio mode ignores quality + force_h264; omit them from the
				// payload so the server validator sees the intended shape.
				quality: isAudio ? undefined : value.quality,
				force_h264: isAudio ? undefined : value.force_h264,
			});
			setOpen(false);
			formApi.reset();
		},
	});

	return (
		<Dialog
			open={open}
			onOpenChange={(next) => {
				setOpen(next);
				// Reset when closing without submit so a re-open starts clean
				// instead of showing the previous selection.
				if (!next) form.reset();
			}}
		>
			<DialogTrigger render={children as React.ReactElement} />
			<DialogContent>
				<form
					onSubmit={(e) => {
						e.preventDefault();
						e.stopPropagation();
						void form.handleSubmit();
					}}
					className="space-y-5"
				>
					<DialogHeader>
						<DialogTitle>{t("videos.trigger_title")}</DialogTitle>
						<DialogDescription>
							{t("videos.trigger_description", { name: broadcasterName })}
						</DialogDescription>
					</DialogHeader>

					<form.Field name="recording_type">
						{(field) => (
							<div className="space-y-2">
								<Label>{t("videos.recording_mode")}</Label>
								<RadioGroup
									value={field.state.value}
									onValueChange={(v) =>
										field.handleChange(
											v as TriggerDownloadFormValues["recording_type"],
										)
									}
									className="flex gap-6"
								>
									<Label htmlFor="mode-video" className="text-sm font-normal">
										<RadioGroupItem value="video" id="mode-video" />
										<span>{t("videos.mode_video")}</span>
									</Label>
									<Label htmlFor="mode-audio" className="text-sm font-normal">
										<RadioGroupItem value="audio" id="mode-audio" />
										<span>{t("videos.mode_audio")}</span>
									</Label>
								</RadioGroup>
							</div>
						)}
					</form.Field>

					<form.Subscribe
						selector={(s) => s.values.recording_type === "audio"}
					>
						{(isAudio) => (
							<>
								<form.Field name="quality">
									{(field) => (
										<div
											className="space-y-2 transition-opacity"
											data-disabled={isAudio || undefined}
										>
											<Label
												htmlFor="quality"
												className={isAudio ? "opacity-50" : undefined}
											>
												{t("videos.quality")}
											</Label>
											<Select
												value={field.state.value}
												onValueChange={(v) =>
													field.handleChange(
														v as TriggerDownloadFormValues["quality"],
													)
												}
												disabled={isAudio}
											>
												<SelectTrigger id="quality" className="w-full">
													<SelectValue />
												</SelectTrigger>
												<SelectContent>
													<SelectItem value="HIGH">
														{t("videos.quality_high")}
													</SelectItem>
													<SelectItem value="MEDIUM">
														{t("videos.quality_medium")}
													</SelectItem>
													<SelectItem value="LOW">
														{t("videos.quality_low")}
													</SelectItem>
												</SelectContent>
											</Select>
										</div>
									)}
								</form.Field>

								<form.Field name="force_h264">
									{(field) => (
										<div
											className="flex items-start gap-2"
											data-disabled={isAudio || undefined}
										>
											<Checkbox
												id="force-h264"
												checked={field.state.value}
												onCheckedChange={(c) => field.handleChange(c === true)}
												disabled={isAudio}
												className="mt-0.5"
											/>
											<div className="flex-1">
												<div className="flex items-center gap-1.5">
													<Label
														htmlFor="force-h264"
														className={isAudio ? "opacity-50" : undefined}
													>
														{t("videos.force_h264")}
													</Label>
													<TooltipProvider>
														<Tooltip>
															<TooltipTrigger
																render={
																	<button
																		type="button"
																		className="text-muted-foreground hover:text-foreground"
																		aria-label={t(
																			"videos.force_h264_tooltip_aria",
																		)}
																	>
																		<Info
																			className="size-3.5"
																			weight="regular"
																		/>
																	</button>
																}
															/>
															<TooltipContent>
																{t("videos.force_h264_tooltip")}
															</TooltipContent>
														</Tooltip>
													</TooltipProvider>
												</div>
											</div>
										</div>
									)}
								</form.Field>
							</>
						)}
					</form.Subscribe>

					{trigger.isError && (
						<div className="rounded-md bg-destructive/10 border border-destructive/20 p-3 text-destructive text-sm">
							{trigger.error?.message ?? t("videos.trigger_failed")}
						</div>
					)}

					<DialogFooter>
						<Button
							type="button"
							variant="outline"
							onClick={() => setOpen(false)}
							disabled={trigger.isPending}
						>
							{t("common.cancel")}
						</Button>
						<form.Subscribe
							selector={(s) => [s.canSubmit, s.isSubmitting] as const}
						>
							{([canSubmit, isSubmitting]) => (
								<Button
									type="submit"
									disabled={!canSubmit || isSubmitting || trigger.isPending}
								>
									{isSubmitting || trigger.isPending
										? t("common.saving")
										: t("videos.trigger_submit")}
								</Button>
							)}
						</form.Subscribe>
					</DialogFooter>
				</form>
			</DialogContent>
		</Dialog>
	);
}
