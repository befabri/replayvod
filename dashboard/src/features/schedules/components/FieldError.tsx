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
