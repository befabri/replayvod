import { useEffect, useMemo, useState } from "react";
import { useTranslation } from "react-i18next";
import type { CategoryResponse } from "@/api/generated/trpc";
import {
	Combobox,
	ComboboxChip,
	ComboboxChipRemove,
	ComboboxChips,
	ComboboxContent,
	ComboboxEmpty,
	ComboboxInput,
	ComboboxItem,
	ComboboxList,
	ComboboxStatus,
} from "@/components/ui/combobox";
import { CategoryBoxArt } from "@/features/categories/components/CategoryBoxArt";
import {
	useCategories,
	useCategorySearch,
} from "@/features/categories/queries";
import { useDebouncedValue } from "@/hooks/useDebouncedValue";

export type CategoryPickerCategory = Pick<
	CategoryResponse,
	"id" | "name" | "box_art_url" | "igdb_id"
>;

interface CategoryMultiPickerProps {
	selected: string[];
	selectedCategories?: CategoryPickerCategory[];
	onChange: (next: string[]) => void;
	disabled?: boolean;
}

const EMPTY_SELECTED_CATEGORIES: CategoryPickerCategory[] = [];

// CategoryMultiPicker drives the schedule form's category filter via a
// Base UI Combobox in multi-select mode. Server-side search keeps the
// dropdown responsive for arbitrarily large Twitch catalogs while the
// selected-item union keeps chips visible across query changes.
function PickerThumb({ url, name }: { url?: string | null; name: string }) {
	return (
		<CategoryBoxArt
			url={url}
			name={name}
			width={24}
			height={32}
			sizes="24px"
			decorative
			placeholderIconSize={14}
			className="w-6 rounded shrink-0"
		/>
	);
}

export function CategoryMultiPicker({
	selected,
	selectedCategories = EMPTY_SELECTED_CATEGORIES,
	onChange,
	disabled,
}: CategoryMultiPickerProps) {
	const { t } = useTranslation();
	const [query, setQuery] = useState("");
	const debounced = useDebouncedValue(query, 200);
	const canSearchTwitch = Array.from(debounced.trim()).length >= 2;
	const { data: results, isFetching } = useCategorySearch(debounced, 50);
	const { data: all } = useCategories();

	// Accumulate every category we've ever seen: edit-form defaults, the full
	// list, plus any search page. Edit defaults matter before useCategories()
	// resolves; without them, interacting during initial load can make unresolved
	// chips disappear from `selectedItems`, and the next onValueChange would write
	// the form back without those IDs.
	const [seen, setSeen] = useState<Map<string, CategoryPickerCategory>>(
		() => new Map(),
	);
	useEffect(() => {
		setSeen((prev) => {
			let next: Map<string, CategoryPickerCategory> | null = null;
			for (const c of [
				...selectedCategories,
				...(all ?? []),
				...(results ?? []),
			]) {
				if (!prev.has(c.id)) {
					next ??= new Map(prev);
					next.set(c.id, c);
				}
			}
			return next ?? prev;
		});
	}, [selectedCategories, all, results]);

	// Resolve the currently-selected ids into full CategoryResponse objects so
	// chips can display the label. Union the seen cache with the freshest list +
	// results so a just-arrived row resolves on the same render the effect commits.
	const selectedItems = useMemo<CategoryPickerCategory[]>(() => {
		const byId = new Map(seen);
		for (const c of selectedCategories) byId.set(c.id, c);
		for (const c of all ?? []) byId.set(c.id, c);
		for (const c of results ?? []) byId.set(c.id, c);
		return selected
			.map((id) => byId.get(id))
			.filter((c): c is CategoryPickerCategory => !!c);
	}, [selected, selectedCategories, seen, all, results]);

	// Stitch selected items into the visible options so they remain
	// deselectable from the dropdown even when they don't match the
	// current query.
	const items = useMemo<CategoryPickerCategory[]>(() => {
		const list = results ?? [];
		const seen = new Set(list.map((c) => c.id));
		return [...list, ...selectedItems.filter((c) => !seen.has(c.id))];
	}, [results, selectedItems]);

	// Visual dim when disabled. Input-blocking is handled by Base UI's
	// own `disabled` prop on Combobox.Root — no `pointer-events-none`
	// wrapper needed.
	return (
		<div className={disabled ? "opacity-50" : undefined}>
			<Combobox<CategoryPickerCategory, true>
				multiple
				items={items}
				filter={null}
				value={selectedItems}
				onValueChange={(list) => onChange(list.map((c) => c.id))}
				onInputValueChange={(v) => setQuery(v)}
				itemToStringLabel={(c) => c.name}
				itemToStringValue={(c) => c.id}
				isItemEqualToValue={(a, b) => a.id === b.id}
				disabled={disabled}
			>
				<ComboboxChips>
					{selectedItems.map((c) => (
						<ComboboxChip key={c.id}>
							{c.name}
							<ComboboxChipRemove />
						</ComboboxChip>
					))}
					<ComboboxInput
						placeholder={t("schedules.search_categories")}
						className="flex-1 min-w-[8rem] border-0 bg-transparent px-0 h-auto shadow-none focus-visible:ring-0 focus-visible:border-0"
					/>
				</ComboboxChips>
				<ComboboxContent>
					<ComboboxList<CategoryPickerCategory>>
						{(item) => (
							<ComboboxItem key={item.id} value={item}>
								<PickerThumb url={item.box_art_url} name={item.name} />
								<span className="truncate">{item.name}</span>
							</ComboboxItem>
						)}
					</ComboboxList>
					{isFetching ? (
						<ComboboxStatus>{t("common.loading")}</ComboboxStatus>
					) : (
						<ComboboxEmpty>
							{canSearchTwitch
								? t("schedules.no_category_matches")
								: t("schedules.no_categories")}
						</ComboboxEmpty>
					)}
				</ComboboxContent>
			</Combobox>
		</div>
	);
}
