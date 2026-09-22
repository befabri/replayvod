export function installMemoryStorage(): Storage {
	const entries = new Map<string, string>();
	const storage: Storage = {
		get length() {
			return entries.size;
		},
		clear: () => entries.clear(),
		getItem: (key) => entries.get(key) ?? null,
		key: (index) => Array.from(entries.keys())[index] ?? null,
		removeItem: (key) => {
			entries.delete(key);
		},
		setItem: (key, value) => {
			entries.set(key, String(value));
		},
	};
	Object.defineProperty(window, "localStorage", {
		configurable: true,
		value: storage,
	});
	return storage;
}
