import { Info } from "@phosphor-icons/react";
import { useState } from "react";
import { useTranslation } from "react-i18next";

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

type RecordingType = "video" | "audio";
type Quality = "HIGH" | "MEDIUM" | "LOW";

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
	const [recordingType, setRecordingType] = useState<RecordingType>("video");
	const [quality, setQuality] = useState<Quality>("HIGH");
	const [forceH264, setForceH264] = useState(false);

	const trigger = useTriggerDownload();
	const isAudio = recordingType === "audio";

	const onSubmit = async (e: React.FormEvent) => {
		e.preventDefault();
		await trigger.mutateAsync({
			broadcaster_id: broadcasterId,
			recording_type: recordingType,
			// Audio mode ignores quality + force_h264; omit them from the
			// payload so the server validator sees the intended shape.
			quality: isAudio ? undefined : quality,
			force_h264: isAudio ? undefined : forceH264,
		});
		setOpen(false);
		setRecordingType("video");
		setQuality("HIGH");
		setForceH264(false);
	};

	return (
		<Dialog open={open} onOpenChange={setOpen}>
			<DialogTrigger render={children as React.ReactElement} />
			<DialogContent>
				<form onSubmit={onSubmit} className="space-y-5">
					<DialogHeader>
						<DialogTitle>{t("videos.trigger_title")}</DialogTitle>
						<DialogDescription>
							{t("videos.trigger_description", { name: broadcasterName })}
						</DialogDescription>
					</DialogHeader>

					<div className="space-y-2">
						<Label>{t("videos.recording_mode")}</Label>
						<RadioGroup
							value={recordingType}
							onValueChange={(v) => setRecordingType(v as RecordingType)}
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
							value={quality}
							onValueChange={(v) => setQuality(v as Quality)}
							disabled={isAudio}
						>
							<SelectTrigger id="quality" className="w-full">
								<SelectValue />
							</SelectTrigger>
							<SelectContent>
								<SelectItem value="HIGH">{t("videos.quality_high")}</SelectItem>
								<SelectItem value="MEDIUM">
									{t("videos.quality_medium")}
								</SelectItem>
								<SelectItem value="LOW">{t("videos.quality_low")}</SelectItem>
							</SelectContent>
						</Select>
					</div>

					<div
						className="flex items-start gap-2"
						data-disabled={isAudio || undefined}
					>
						<Checkbox
							id="force-h264"
							checked={forceH264}
							onCheckedChange={(c) => setForceH264(c === true)}
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
													aria-label={t("videos.force_h264_tooltip_aria")}
												>
													<Info className="size-3.5" weight="regular" />
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
						<Button type="submit" disabled={trigger.isPending}>
							{trigger.isPending
								? t("common.saving")
								: t("videos.trigger_submit")}
						</Button>
					</DialogFooter>
				</form>
			</DialogContent>
		</Dialog>
	);
}
