// Formatting helpers kept in a separate file so the table/player components
// can import them without pulling in all of the queries module.

export function formatBytes(bytes: number | undefined | null): string {
	if (bytes == null || bytes <= 0) return "—";
	const units = ["B", "KB", "MB", "GB", "TB"];
	let v = bytes;
	let i = 0;
	while (v >= 1024 && i < units.length - 1) {
		v /= 1024;
		i++;
	}
	return `${v.toFixed(v >= 10 || i === 0 ? 0 : 1)} ${units[i]}`;
}

// formatAverageBitrate derives an average bitrate from total bytes
// and total seconds. Not the encoder bitrate — there's no stored
// "encode bitrate" on the recording — but it's the right order of
// magnitude and matches what tools like ffprobe report under
// `format.bit_rate`.
export function formatAverageBitrate(
	sizeBytes: number | undefined | null,
	durationSeconds: number | undefined | null,
): string {
	if (!sizeBytes || !durationSeconds || sizeBytes <= 0 || durationSeconds <= 0)
		return "—";
	const bps = (sizeBytes * 8) / durationSeconds;
	if (bps >= 1_000_000) return `${(bps / 1_000_000).toFixed(1)} Mb/s`;
	if (bps >= 1_000) return `${Math.round(bps / 1_000)} kb/s`;
	return `${Math.round(bps)} b/s`;
}

export function formatDuration(seconds: number | undefined | null): string {
	if (seconds == null || seconds <= 0) return "—";
	const h = Math.floor(seconds / 3600);
	const m = Math.floor((seconds % 3600) / 60);
	const s = Math.floor(seconds % 60);
	if (h > 0) {
		return `${h}:${m.toString().padStart(2, "0")}:${s.toString().padStart(2, "0")}`;
	}
	return `${m}:${s.toString().padStart(2, "0")}`;
}
