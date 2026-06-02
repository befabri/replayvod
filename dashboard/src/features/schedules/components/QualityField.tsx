import { useTranslation } from "react-i18next";
import { Label } from "@/components/ui/label";
import {
	Select,
	SelectContent,
	SelectItem,
	SelectTrigger,
	SelectValue,
} from "@/components/ui/select";
import { cn } from "@/lib/utils";
import type { ScheduleFormApi } from "../form";
import { scheduleQualityOptions, scheduleQualityValue } from "../quality";

// QualityField is the recording-quality <Select>, shared verbatim by the
// create and edit forms. The picker value is constrained to the rendered
// options, but Base UI's onValueChange widens it to a loose value, so
// coerce back to the schema enum (falling back to the default) before
// handing it to the typed field.
export function QualityField({
	form,
	className,
}: {
	form: ScheduleFormApi;
	className?: string;
}) {
	const { t } = useTranslation();
	const options = scheduleQualityOptions(t);
	return (
		<form.Field name="quality">
			{(field) => (
				<div className={cn("flex flex-col gap-1", className)}>
					<Label htmlFor={field.name} className="text-muted-foreground">
						{t("schedules.quality")}
					</Label>
					<Select
						value={field.state.value}
						onValueChange={(v) =>
							field.handleChange(scheduleQualityValue(String(v)))
						}
					>
						<SelectTrigger id={field.name}>
							<SelectValue />
						</SelectTrigger>
						<SelectContent>
							{options.map((option) => (
								<SelectItem key={option.value} value={option.value}>
									{option.label}
								</SelectItem>
							))}
						</SelectContent>
					</Select>
				</div>
			)}
		</form.Field>
	);
}
