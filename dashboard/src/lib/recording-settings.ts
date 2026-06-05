export type RecordingMode = "video" | "audio";
export type RecordingQuality = "LOW" | "MEDIUM" | "HIGH";

export function isAudioRecording(recordingType: RecordingMode | string) {
	return recordingType === "audio";
}

export function forceH264For(
	recordingType: RecordingMode | string,
	forceH264: boolean,
) {
	return isAudioRecording(recordingType) ? false : forceH264;
}
