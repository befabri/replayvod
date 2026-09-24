import { MagnifyingGlassIcon, XIcon } from "@phosphor-icons/react";
import { type KeyboardEvent, useMemo, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { Checkbox } from "./checkbox";
import { Label } from "./label";

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
	noMatchesHint,
	disabled,
	"aria-label": ariaLabel,
}: {
	options: PickerOption<T>[];
	selected: T[];
	onChange: (next: T[]) => void;
	placeholder: string;
	emptyHint: string;
	noMatchesHint: string;
	disabled?: boolean;
	"aria-label": string;
}) {
	const { t } = useTranslation();
	const [query, setQuery] = useState("");
	const searchRef = useRef<HTMLInputElement>(null);
	const listRef = useRef<HTMLDivElement>(null);
	const selectedSet = useMemo(() => new Set(selected), [selected]);
	const selectedOptions = useMemo(
		() => options.filter((option) => selectedSet.has(option.id)),
		[options, selectedSet],
	);
	const filtered = useMemo(() => {
		const q = query.trim().toLocaleLowerCase();
		if (!q) return options;
		return options.filter((option) =>
			option.label.toLocaleLowerCase().includes(q),
		);
	}, [options, query]);

	const setChecked = (id: T, checked: boolean) => {
		if (checked === selectedSet.has(id)) return;
		onChange(checked ? [...selected, id] : selected.filter((x) => x !== id));
	};

	const optionBoxes = () => [
		...(listRef.current?.querySelectorAll<HTMLElement>('[role="checkbox"]') ??
			[]),
	];

	const moveFocusTo = (
		event: KeyboardEvent,
		target: HTMLElement | null | undefined,
	) => {
		if (!target) return;
		event.preventDefault();
		target.focus();
	};

	const handleSearchKeyDown = (event: KeyboardEvent<HTMLInputElement>) => {
		if (event.key === "ArrowDown") moveFocusTo(event, optionBoxes()[0]);
	};

	const handleOptionKeyDown = (event: KeyboardEvent, index: number) => {
		if (event.key === "ArrowDown") {
			moveFocusTo(event, optionBoxes()[index + 1]);
		} else if (event.key === "ArrowUp") {
			moveFocusTo(
				event,
				index === 0 ? searchRef.current : optionBoxes()[index - 1],
			);
		}
	};

	return (
		<fieldset
			aria-label={ariaLabel}
			disabled={disabled}
			data-slot="multi-select-picker"
			data-dimmed={disabled || undefined}
			className="min-w-0 rounded-md border border-border bg-background shadow-xs transition-colors has-[input[type=search]:focus-visible]:border-ring has-[input[type=search]:focus-visible]:ring-[3px] has-[input[type=search]:focus-visible]:ring-ring/50 data-dimmed:pointer-events-none data-dimmed:opacity-50"
		>
			{selectedOptions.length > 0 ? (
				<ul className="flex flex-wrap gap-1.5 border-b border-border p-2">
					{selectedOptions.map((option) => (
						<li
							key={String(option.id)}
							className="inline-flex items-center gap-1 rounded-md bg-primary/20 py-0.5 pr-1 pl-2 text-xs text-foreground"
						>
							{option.label}
							<button
								type="button"
								onClick={() => setChecked(option.id, false)}
								aria-label={t("common.remove_item", { item: option.label })}
								className="rounded-sm p-0.5 opacity-60 transition-opacity outline-none hover:opacity-100 focus-visible:opacity-100 focus-visible:ring-2 focus-visible:ring-ring"
							>
								<XIcon aria-hidden="true" className="size-3" />
							</button>
						</li>
					))}
				</ul>
			) : null}
			<div className="relative">
				<MagnifyingGlassIcon
					aria-hidden="true"
					className="pointer-events-none absolute top-1/2 left-3 size-4 -translate-y-1/2 text-muted-foreground"
				/>
				<input
					ref={searchRef}
					type="search"
					value={query}
					onChange={(event) => setQuery(event.target.value)}
					onKeyDown={handleSearchKeyDown}
					placeholder={placeholder}
					aria-label={placeholder}
					className="h-9 w-full bg-transparent pr-3 pl-9 text-sm outline-none placeholder:text-muted-foreground [&::-webkit-search-cancel-button]:hidden"
				/>
			</div>
			{disabled ? null : (
				<div
					ref={listRef}
					className="max-h-48 overflow-y-auto border-t border-border p-1"
				>
					{filtered.length === 0 ? (
						<p className="px-2 py-1.5 text-xs text-muted-foreground">
							{options.length === 0 ? emptyHint : noMatchesHint}
						</p>
					) : (
						filtered.map((option, index) => (
							<Label
								key={String(option.id)}
								className="cursor-pointer rounded-sm px-2 py-1.5 font-normal leading-normal hover:bg-accent"
							>
								<Checkbox
									checked={selectedSet.has(option.id)}
									onCheckedChange={(checked) => setChecked(option.id, checked)}
									onKeyDown={(event) => handleOptionKeyDown(event, index)}
								/>
								<span className="truncate">{option.label}</span>
							</Label>
						))
					)}
				</div>
			)}
		</fieldset>
	);
}
