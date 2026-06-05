import { InfoIcon } from "@phosphor-icons/react";
import { useId } from "react";
import { useTranslation } from "react-i18next";
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
	type RecordingMode,
	type RecordingQuality,
} from "@/lib/recording-settings";
import { cn } from "@/lib/utils";

const RECORDING_QUALITIES: readonly RecordingQuality[] = [
	"HIGH",
	"MEDIUM",
	"LOW",
];

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
}: {
	recordingType: RecordingMode;
	onRecordingTypeChange: (value: RecordingMode) => void;
	quality: RecordingQuality;
	onQualityChange: (value: RecordingQuality) => void;
	forceH264: boolean;
	onForceH264Change: (value: boolean) => void;
	tBase: string;
	disabled?: boolean;
}) {
	const { t } = useTranslation();
	const id = useId();
	const settingsDisabled = disabled || isAudioRecording(recordingType);

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

			<div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
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
					<Select
						value={quality}
						onValueChange={(v) => onQualityChange(v as RecordingQuality)}
						disabled={settingsDisabled}
					>
						<SelectTrigger id={`${id}-quality`}>
							<SelectValue />
						</SelectTrigger>
						<SelectContent>
							{RECORDING_QUALITIES.map((value) => (
								<SelectItem key={value} value={value}>
									{t(`${tBase}.quality_${value.toLowerCase()}`)}
								</SelectItem>
							))}
						</SelectContent>
					</Select>
				</div>

				<div className="flex items-start gap-2 pt-1 sm:pt-7">
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
