import { useMemo, useState } from "react";
import { useTranslation } from "react-i18next";
import { Avatar } from "@/components/ui/avatar";
import {
	Combobox,
	ComboboxContent,
	ComboboxEmpty,
	ComboboxInput,
	ComboboxItem,
	ComboboxList,
	ComboboxStatus,
} from "@/components/ui/combobox";
import { useChannel, useChannelSearch } from "@/features/channels/queries";
import type { ChannelResponse } from "@/features/channels/types";
import { useDebouncedValue } from "@/hooks/useDebouncedValue";

interface ChannelPickerProps {
	value: string;
	onChange: (broadcasterId: string) => void;
	id?: string;
	placeholder?: string;
	"aria-invalid"?: boolean;
}

// Base UI Combobox clears by passing null to onValueChange; we map that to
// an empty string so consumers stay on a simple string contract.

export function ChannelPicker({
	value,
	onChange,
	id,
	placeholder,
	"aria-invalid": ariaInvalid,
}: ChannelPickerProps) {
	const { t } = useTranslation();
	const [query, setQuery] = useState("");
	const debounced = useDebouncedValue(query, 200);
	const { data: results, isFetching } = useChannelSearch(debounced, 20);
	const { data: selectedChannel } = useChannel(value);

	// Stitch the currently-selected channel into the options list so its
	// value stays resolvable even when it's outside the active search page.
	const items = useMemo<ChannelResponse[]>(() => {
		const list = results ?? [];
		if (!selectedChannel) return list;
		if (list.some((c) => c.broadcaster_id === selectedChannel.broadcaster_id)) {
			return list;
		}
		return [selectedChannel, ...list];
	}, [results, selectedChannel]);

	return (
		<Combobox<ChannelResponse>
			items={items}
			filter={null}
			value={selectedChannel ?? null}
			onValueChange={(channel) => onChange(channel?.broadcaster_id ?? "")}
			onInputValueChange={(v) => setQuery(v)}
			itemToStringLabel={(c) => c.broadcaster_name}
			itemToStringValue={(c) => c.broadcaster_id}
			isItemEqualToValue={(a, b) => a.broadcaster_id === b.broadcaster_id}
		>
			<ComboboxInput
				id={id}
				placeholder={placeholder ?? t("schedules.search_channels")}
				aria-invalid={ariaInvalid}
			/>
			<ComboboxContent>
				<ComboboxList<ChannelResponse>>
					{(item) => (
						<ComboboxItem key={item.broadcaster_id} value={item}>
							<Avatar
								src={item.profile_image_url}
								name={item.broadcaster_name}
								size="sm"
							/>
							<div className="flex flex-col min-w-0 flex-1">
								<span className="truncate font-medium">
									{item.broadcaster_name}
								</span>
								<span className="truncate text-xs text-muted-foreground font-mono">
									{item.broadcaster_login}
								</span>
							</div>
						</ComboboxItem>
					)}
				</ComboboxList>
				{isFetching ? (
					<ComboboxStatus>{t("common.loading")}</ComboboxStatus>
				) : (
					<ComboboxEmpty>{t("schedules.no_channels_found")}</ComboboxEmpty>
				)}
			</ComboboxContent>
		</Combobox>
	);
}
