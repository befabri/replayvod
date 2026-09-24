export function allOf<T extends string>(members: Record<T, true>): T[] {
	return Object.keys(members) as T[];
}
