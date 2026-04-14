// FieldError renders the first validation error TanStack Form surfaces.
// Errors come through as Zod issues — stringifying is enough for the
// homelab UI; internationalized field errors would layer on top.
export function FieldError({ errors }: { errors: readonly unknown[] }) {
	if (errors.length === 0) return null;
	const first = errors[0];
	const msg =
		typeof first === "string"
			? first
			: first && typeof first === "object" && "message" in first
				? String((first as { message: unknown }).message)
				: "Invalid";
	return <span className="text-xs text-destructive">{msg}</span>;
}
