import { useMemo, useState } from "react";
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
import {
	useCategories,
	useCategorySearch,
} from "@/features/categories/queries";
import { useDebouncedValue } from "@/hooks/useDebouncedValue";
import { resolveBoxArtUrl } from "@/lib/twitch";

interface CategoryMultiPickerProps {
	selected: string[];
	onChange: (next: string[]) => void;
	disabled?: boolean;
}

// CategoryMultiPicker drives the schedule form's category filter via a
// Base UI Combobox in multi-select mode. Server-side search (once the
// backend exposes category.search) keeps the dropdown responsive for
// arbitrarily large Twitch catalogs; until then it client-filters the
// list response.
// Small box-art thumbnail used in the dropdown rows. Silently drops to
// a bg-muted block if the URL is missing or fails to load.
function PickerThumb({ url }: { url?: string | null }) {
	const resolved = resolveBoxArtUrl(url, 36, 48);
	const [errored, setErrored] = useState(false);
	if (!resolved || errored) {
		return <span className="w-6 h-8 rounded bg-muted shrink-0" />;
	}
	return (
		<img
			src={resolved}
			alt=""
			className="w-6 h-8 object-cover rounded shrink-0"
			onError={() => setErrored(true)}
		/>
	);
}

export function CategoryMultiPicker({
	selected,
	onChange,
	disabled,
}: CategoryMultiPickerProps) {
	const { t } = useTranslation();
	const [query, setQuery] = useState("");
	const debounced = useDebouncedValue(query, 200);
	const { data: results, isFetching } = useCategorySearch(debounced, 50);
	const { data: all } = useCategories();

	// Resolve the currently-selected ids into full CategoryResponse
	// objects so chips can display the label. We union `useCategories`
	// data and the current search results — a category picked from a
	// prior search page may not be in either alone, so pulling from
	// both maximizes coverage. Once the backend `category.search` lands
	// and the list endpoint stops being sparse, this will just work.
	const selectedItems = useMemo<CategoryResponse[]>(() => {
		const byId = new Map<string, CategoryResponse>();
		for (const c of all ?? []) byId.set(c.id, c);
		for (const c of results ?? []) byId.set(c.id, c);
		return selected
			.map((id) => byId.get(id))
			.filter((c): c is CategoryResponse => !!c);
	}, [selected, all, results]);

	// Stitch selected items into the visible options so they remain
	// deselectable from the dropdown even when they don't match the
	// current query.
	const items = useMemo<CategoryResponse[]>(() => {
		const list = results ?? [];
		const seen = new Set(list.map((c) => c.id));
		return [...list, ...selectedItems.filter((c) => !seen.has(c.id))];
	}, [results, selectedItems]);

	// Visual dim when disabled. Input-blocking is handled by Base UI's
	// own `disabled` prop on Combobox.Root — no `pointer-events-none`
	// wrapper needed.
	return (
		<div className={disabled ? "opacity-50" : undefined}>
			<Combobox<CategoryResponse, true>
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
					<ComboboxList<CategoryResponse>>
						{(item) => (
							<ComboboxItem key={item.id} value={item}>
								<PickerThumb url={item.box_art_url} />
								<span className="truncate">{item.name}</span>
							</ComboboxItem>
						)}
					</ComboboxList>
					{isFetching ? (
						<ComboboxStatus>{t("common.loading")}</ComboboxStatus>
					) : (
						<ComboboxEmpty>{t("schedules.no_categories")}</ComboboxEmpty>
					)}
				</ComboboxContent>
			</Combobox>
		</div>
	);
}
