import { useTranslation } from "react-i18next";
import { Checkbox } from "@/components/ui/checkbox";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import {
	MultiSelectPicker,
	type PickerOption,
} from "@/components/ui/multi-select-picker";
import { CategoryMultiPicker } from "@/features/categories/components/CategoryMultiPicker";
import { useTags } from "@/features/tags";
import { FieldError } from "./FieldError";

// FiltersFieldset renders the shared filter controls used by both the
// create and edit forms. Uses `any` on the form prop because TanStack
// Form's inferred signature depends on a closure-captured validators
// arg; typing it explicitly balloons the generic shape without payoff
// for a one-file-shared component.
//
// Inputs are always rendered (never conditionally mounted) to avoid
// layout shift when toggling the gating checkboxes. When the checkbox
// is unchecked the corresponding input is disabled + dimmed.
// biome-ignore lint/suspicious/noExplicitAny: See above
export function FiltersFieldset({ form }: { form: any }) {
	const { t } = useTranslation();
	const tagsQuery = useTags();

	const tagOptions: PickerOption<number>[] = (tagsQuery.data ?? []).map(
		(tag) => ({
			id: tag.id,
			label: tag.name,
		}),
	);

	return (
		<fieldset className="space-y-4">
			<legend className="text-xs text-muted-foreground pb-2">
				{t("schedules.filters")}
			</legend>

			<Row>
				<form.Field name="is_delete_rediff">
					{(field: any) => (
						<ToggleRow
							checked={field.state.value}
							onChange={(c) => {
								field.handleChange(c);
								// Clear the gated input when toggling off so a later
								// re-toggle doesn't surface stale values, and a submit
								// while off doesn't silently wipe what the user saw.
								if (!c) form.setFieldValue("time_before_delete", undefined);
							}}
							label={t("schedules.is_delete_rediff")}
							hint={t("schedules.is_delete_rediff_hint")}
						/>
					)}
				</form.Field>
				<form.Subscribe selector={(s: any) => s.values.is_delete_rediff}>
					{(enabled: boolean) => (
						<form.Field name="time_before_delete">
							{(field: any) => (
								<NumberInputRow
									id={field.name}
									label={t("schedules.time_before_delete")}
									value={field.state.value}
									onChange={(v) => field.handleChange(v)}
									disabled={!enabled}
									min={1}
									errors={field.state.meta.errors}
								/>
							)}
						</form.Field>
					)}
				</form.Subscribe>
			</Row>

			<Row>
				<form.Field name="has_min_viewers">
					{(field: any) => (
						<ToggleRow
							checked={field.state.value}
							onChange={(c) => {
								field.handleChange(c);
								if (!c) form.setFieldValue("min_viewers", undefined);
							}}
							label={t("schedules.has_min_viewers")}
						/>
					)}
				</form.Field>
				<form.Subscribe selector={(s: any) => s.values.has_min_viewers}>
					{(enabled: boolean) => (
						<form.Field name="min_viewers">
							{(field: any) => (
								<NumberInputRow
									id={field.name}
									label={t("schedules.min_viewers")}
									value={field.state.value}
									onChange={(v) => field.handleChange(v)}
									disabled={!enabled}
									min={0}
									errors={field.state.meta.errors}
								/>
							)}
						</form.Field>
					)}
				</form.Subscribe>
			</Row>

			<Row>
				<form.Field name="has_categories">
					{(field: any) => (
						<ToggleRow
							checked={field.state.value}
							onChange={(c) => {
								field.handleChange(c);
								if (!c) form.setFieldValue("category_ids", []);
							}}
							label={t("schedules.has_categories")}
						/>
					)}
				</form.Field>
				<form.Subscribe selector={(s: any) => s.values.has_categories}>
					{(enabled: boolean) => (
						<form.Field name="category_ids">
							{(field: any) => (
								<div className="pl-6">
									<CategoryMultiPicker
										selected={field.state.value ?? []}
										onChange={(next) => field.handleChange(next)}
										disabled={!enabled}
									/>
								</div>
							)}
						</form.Field>
					)}
				</form.Subscribe>
			</Row>

			<Row>
				<form.Field name="has_tags">
					{(field: any) => (
						<ToggleRow
							checked={field.state.value}
							onChange={(c) => {
								field.handleChange(c);
								if (!c) form.setFieldValue("tag_ids", []);
							}}
							label={t("schedules.has_tags")}
						/>
					)}
				</form.Field>
				<form.Subscribe selector={(s: any) => s.values.has_tags}>
					{(enabled: boolean) => (
						<form.Field name="tag_ids">
							{(field: any) => (
								<MultiSelectPicker<number>
									options={tagOptions}
									selected={field.state.value ?? []}
									onChange={(next) => field.handleChange(next)}
									placeholder={t("schedules.search_tags")}
									emptyHint={t("schedules.no_tags")}
									disabled={!enabled}
								/>
							)}
						</form.Field>
					)}
				</form.Subscribe>
			</Row>
		</fieldset>
	);
}

function Row({ children }: { children: React.ReactNode }) {
	return <div className="space-y-2">{children}</div>;
}

function ToggleRow({
	checked,
	onChange,
	label,
	hint,
}: {
	checked: boolean;
	onChange: (c: boolean) => void;
	label: string;
	hint?: string;
}) {
	return (
		<div>
			<Label className="flex items-center gap-2 text-sm font-normal">
				<Checkbox
					checked={checked}
					onCheckedChange={(c: boolean) => onChange(c === true)}
				/>
				{label}
			</Label>
			{hint ? (
				<span className="block pl-6 text-xs text-muted-foreground">{hint}</span>
			) : null}
		</div>
	);
}

function NumberInputRow({
	id,
	label,
	value,
	onChange,
	disabled,
	min,
	errors,
}: {
	id: string;
	label: string;
	value: number | undefined;
	onChange: (next: number | undefined) => void;
	disabled?: boolean;
	min: number;
	errors: unknown[];
}) {
	return (
		<div
			className={`flex flex-col gap-1 max-w-xs pl-6 ${disabled ? "opacity-50" : ""}`}
		>
			<Label htmlFor={id} className="text-xs text-muted-foreground">
				{label}
			</Label>
			<Input
				id={id}
				type="number"
				min={min}
				disabled={disabled}
				value={value ?? ""}
				onChange={(e: React.ChangeEvent<HTMLInputElement>) =>
					onChange(e.target.value === "" ? undefined : Number(e.target.value))
				}
				aria-invalid={errors.length > 0 ? true : undefined}
			/>
			<FieldError errors={errors} />
		</div>
	);
}
