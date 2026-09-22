import { useCallback, useState } from "react";

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
			} catch {}
		},
		[key, serialize],
	);

	return [value, set];
}
