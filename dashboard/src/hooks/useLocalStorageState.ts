import { useCallback, useState } from "react";

// useLocalStorageState is useState backed by localStorage. The stored value is
// read once via the lazy initializer (no mount effect, no hydrated-ref to
// suppress the first write), and every setter call writes through. `parse` and
// `serialize` are paired so non-string values can round-trip without relying on
// String(value).
export function useLocalStorageState<T>(
	key: string,
	fallback: T,
	parse: (raw: string) => T | null,
	serialize: (value: T) => string,
): [T, (value: T) => void] {
	const [value, setValue] = useState<T>(() => {
		try {
			const raw = window.localStorage.getItem(key);
			if (raw == null) return fallback;
			return parse(raw) ?? fallback;
		} catch {
			return fallback;
		}
	});

	const set = useCallback(
		(next: T) => {
			setValue(next);
			try {
				window.localStorage.setItem(key, serialize(next));
			} catch {
				// Storage unavailable (private mode / quota) — in-memory state still
				// updates; the preference just won't persist.
			}
		},
		[key, serialize],
	);

	return [value, set];
}
