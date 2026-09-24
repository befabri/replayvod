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
	qualityPlaceholder,
}: {
	recordingType: RecordingMode;
	onRecordingTypeChange: (value: RecordingMode) => void;
	quality: RecordingQuality | null;
	onQualityChange: (value: RecordingQuality) => void;
	forceH264: boolean;
	onForceH264Change: (value: boolean) => void;
	tBase: string;
	disabled?: boolean;
	qualityPlaceholder?: string;
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
									<SelectValue placeholder={qualityPlaceholder} />
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
							{!settingsDisabled && quality !== null && (
								<QualitySessionHint quality={quality} />
							)}
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
			data-dimmed={disabled || undefined}
			className="text-sm font-normal data-dimmed:opacity-50"
		>
			<RadioGroupItem value={value} id={id} />
			<span>{label}</span>
		</Label>
	);
}
