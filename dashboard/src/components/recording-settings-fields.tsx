import { InfoIcon } from "@phosphor-icons/react";
import { type ReactNode, useId } from "react";
import { useTranslation } from "react-i18next";
import { QualitySessionHint } from "@/components/quality-session-hint";
import { Checkbox } from "@/components/ui/checkbox";
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
import {
	forceH264For,
	isAudioRecording,
	RECORDING_QUALITIES,
	type RecordingMode,
	type RecordingQuality,
} from "@/lib/recording-settings";
import { cn } from "@/lib/utils";

// RecordingSettingsFields renders the mode / quality / Force H.264 controls
// shared by the schedule forms and the "download now" surfaces. It is
// presentational: it takes the current values plus change callbacks rather than
// a form instance, so the two features (which use different TanStack forms) can
// each bind it without leaking their form type here.
//
// Quality and the H.264 override are video-only: switching to audio greys them
// out and clears force_h264 so a stale checked box can't sit behind the
// disabled control. The server enforces the same audio rule
// (repository.ScheduleForceH264) and ignores quality for audio, so this is the
// UX mirror, not the source of truth. `disabled` greys the whole block at once
// (the channel dialog passes it when the channel is offline).
//
// The quality ladder is a list of ceilings, and says so under the picker. A
// ceiling above 1080p may need the owner's Twitch session, so
// QualitySessionHint sits there too whenever such a quality is live (video
// mode, block enabled); see that component for who sees what.
//
// `qualityPicker` swaps the ladder for another control under the same label,
// which the "download now" surface uses to list what the live stream offers.
// The ladder's hints go with it: the replacement explains itself.
//
// `tBase` is the i18n namespace ("schedules" or "videos"); both expose the same
// recording_mode / mode_* / quality / quality_* / force_h264* keys.
export function RecordingSettingsFields({
	recordingType,
	onRecordingTypeChange,
	quality,
	onQualityChange,
	forceH264,
	onForceH264Change,
	tBase,
	disabled = false,
	qualityPicker,
}: {
	recordingType: RecordingMode;
	onRecordingTypeChange: (value: RecordingMode) => void;
	quality: RecordingQuality;
	onQualityChange: (value: RecordingQuality) => void;
	forceH264: boolean;
	onForceH264Change: (value: boolean) => void;
	tBase: string;
	disabled?: boolean;
	qualityPicker?: (props: { id: string; disabled: boolean }) => ReactNode;
}) {
	const { t } = useTranslation();
	const id = useId();
	const settingsDisabled = disabled || isAudioRecording(recordingType);
	const qualityItems = RECORDING_QUALITIES.map((value) => ({
		value,
		label: t(`${tBase}.quality_${value.toLowerCase()}`),
	}));

	const handleMode = (value: RecordingMode) => {
		onRecordingTypeChange(value);
		const nextForceH264 = forceH264For(value, forceH264);
		if (nextForceH264 !== forceH264) onForceH264Change(nextForceH264);
	};

	return (
		<>
			<div className="space-y-2">
				<Label
					className={cn("text-muted-foreground", disabled && "opacity-50")}
				>
					{t(`${tBase}.recording_mode`)}
				</Label>
				<RadioGroup
					value={recordingType}
					onValueChange={(v) => handleMode(v as RecordingMode)}
					disabled={disabled}
					className="flex flex-wrap gap-6"
				>
					<ModeOption
						id={`${id}-mode-video`}
						value="video"
						label={t(`${tBase}.mode_video`)}
						disabled={disabled}
					/>
					<ModeOption
						id={`${id}-mode-audio`}
						value="audio"
						label={t(`${tBase}.mode_audio`)}
						disabled={disabled}
					/>
				</RadioGroup>
			</div>

			<div className="grid gap-3">
				<div className="flex flex-col gap-1">
					<Label
						htmlFor={`${id}-quality`}
						className={cn(
							"text-muted-foreground",
							settingsDisabled && "opacity-50",
						)}
					>
						{t(`${tBase}.quality`)}
					</Label>
					{qualityPicker ? (
						qualityPicker({
							id: `${id}-quality`,
							disabled: settingsDisabled,
						})
					) : (
						<>
							<Select
								value={quality}
								onValueChange={(v) => onQualityChange(v as RecordingQuality)}
								disabled={settingsDisabled}
								items={qualityItems}
							>
								<SelectTrigger id={`${id}-quality`}>
									<SelectValue />
								</SelectTrigger>
								<SelectContent>
									{qualityItems.map((item) => (
										<SelectItem key={item.value} value={item.value}>
											{item.label}
										</SelectItem>
									))}
								</SelectContent>
							</Select>
							<p
								className={cn(
									"text-xs text-muted-foreground",
									settingsDisabled && "opacity-50",
								)}
							>
								{t(`${tBase}.quality_limit_hint`)}
							</p>
							{!settingsDisabled && <QualitySessionHint quality={quality} />}
						</>
					)}
				</div>

				<div className="flex items-start gap-2">
					<Checkbox
						id={`${id}-force-h264`}
						checked={forceH264}
						onCheckedChange={(c) => onForceH264Change(c === true)}
						disabled={settingsDisabled}
						className="mt-0.5"
					/>
					<div className="flex-1">
						<div className="flex items-center gap-1.5">
							<Label
								htmlFor={`${id}-force-h264`}
								className={cn(
									"text-sm font-normal",
									settingsDisabled && "opacity-50",
								)}
							>
								{t(`${tBase}.force_h264`)}
							</Label>
							<TooltipProvider>
								<Tooltip>
									<TooltipTrigger
										render={
											<button
												type="button"
												className="text-muted-foreground hover:text-foreground"
												aria-label={t(`${tBase}.force_h264_tooltip_aria`)}
											>
												<InfoIcon className="size-3.5" weight="regular" />
											</button>
										}
									/>
									<TooltipContent>
										{t(`${tBase}.force_h264_tooltip`)}
									</TooltipContent>
								</Tooltip>
							</TooltipProvider>
						</div>
					</div>
				</div>
			</div>
		</>
	);
}

function ModeOption({
	id,
	value,
	label,
	disabled,
}: {
	id: string;
	value: RecordingMode;
	label: string;
	disabled?: boolean;
}) {
	return (
		<Label
			htmlFor={id}
			className={cn("text-sm font-normal", disabled && "opacity-50")}
		>
			<RadioGroupItem value={value} id={id} />
			<span>{label}</span>
		</Label>
	);
}
