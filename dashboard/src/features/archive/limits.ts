// The server accepts at most this many VODs per archive.enqueue call. The
// paste box refuses more; the channel browser splits a larger selection.
export const MAX_VODS_PER_ENQUEUE = 50;

/** Splits items into consecutive groups of at most size. */
export function chunk<T>(items: readonly T[], size: number): T[][] {
	const out: T[][] = [];
	for (let i = 0; i < items.length; i += size) {
		out.push(items.slice(i, i + size));
	}
	return out;
}
