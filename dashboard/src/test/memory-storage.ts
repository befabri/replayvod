// installMemoryStorage swaps window.localStorage for an in-memory Storage so
// tests do not depend on the runtime's own: under vitest's jsdom environment
// the global is Node's stub, which lacks the Web Storage methods.
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
