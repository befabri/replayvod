import { useMemo, useState } from "react";

// MultiSelectPicker is a simple searchable multi-select used by the
// schedule create/edit forms to pick categories and tags. Deliberately
// minimal: a search input, scrollable option list, toggleable rows,
// plus a chip display of the current selection. No Radix/Base UI
// combobox — the interaction is light enough that raw inputs keep
// the code short and accessible-by-default.
export type PickerOption<T extends string | number> = {
	id: T;
	label: string;
};

export function MultiSelectPicker<T extends string | number>({
	options,
	selected,
	onChange,
	placeholder,
	emptyHint,
	disabled,
}: {
	options: PickerOption<T>[];
	selected: T[];
	onChange: (next: T[]) => void;
	placeholder?: string;
	emptyHint?: string;
	disabled?: boolean;
}) {
	const [query, setQuery] = useState("");
	const selectedSet = useMemo(() => new Set(selected), [selected]);
	const filtered = useMemo(() => {
		const q = query.trim().toLowerCase();
		if (!q) return options;
		return options.filter((o) => o.label.toLowerCase().includes(q));
	}, [options, query]);

	const selectedOptions = useMemo(
		() => options.filter((o) => selectedSet.has(o.id)),
		[options, selectedSet],
	);

	const toggle = (id: T) => {
		if (disabled) return;
		if (selectedSet.has(id)) {
			onChange(selected.filter((x) => x !== id));
		} else {
			onChange([...selected, id]);
		}
	};

	return (
		<div
			className={`space-y-2 ${disabled ? "opacity-50 pointer-events-none" : ""}`}
			aria-disabled={disabled}
		>
			{selectedOptions.length > 0 && (
				<div className="flex flex-wrap gap-1.5">
					{selectedOptions.map((o) => (
						<button
							key={String(o.id)}
							type="button"
							onClick={() => toggle(o.id)}
							className="inline-flex items-center gap-1.5 rounded-md bg-primary/20 text-foreground px-2 py-0.5 text-xs hover:bg-primary/30"
						>
							{o.label}
							<span aria-hidden="true" className="opacity-60">
								×
							</span>
						</button>
					))}
				</div>
			)}
			<input
				type="text"
				value={query}
				onChange={(e) => setQuery(e.target.value)}
				placeholder={placeholder ?? "Search…"}
				disabled={disabled}
				className="w-full rounded-md border border-border bg-background px-3 py-2 text-sm disabled:cursor-not-allowed"
			/>
			<div className="max-h-40 overflow-y-auto rounded-md border border-border bg-background/50">
				{filtered.length === 0 ? (
					<div className="px-3 py-2 text-xs text-muted-foreground">
						{emptyHint ?? "No matches."}
					</div>
				) : (
					<ul>
						{filtered.map((o) => {
							const isSelected = selectedSet.has(o.id);
							return (
								<li key={String(o.id)}>
									<button
										type="button"
										onClick={() => toggle(o.id)}
										className={`w-full text-left px-3 py-1.5 text-sm flex items-center gap-2 hover:bg-muted ${
											isSelected ? "text-foreground" : "text-muted-foreground"
										}`}
									>
										<input
											type="checkbox"
											readOnly
											checked={isSelected}
											tabIndex={-1}
											className="pointer-events-none"
										/>
										<span className="truncate flex-1">{o.label}</span>
									</button>
								</li>
							);
						})}
					</ul>
				)}
			</div>
		</div>
	);
}
