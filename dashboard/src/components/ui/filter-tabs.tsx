import { Tabs, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { cn } from "@/lib/utils";

export type FilterTabOption = {
	value: string;
	label: string;
	// Optional trailing count. Omit (undefined) to render no count, e.g. while
	// the statistics aggregate is still loading.
	count?: number;
};

// FilterTabs is the dashboard's underline tab strip: a horizontal row of
// triggers with an active underline and an optional trailing count. Shared by
// the videos library scope tabs and the history filter so the visual has a
// single source of truth.
export function FilterTabs({
	value,
	options,
	onChange,
	className,
}: {
	value: string;
	options: FilterTabOption[];
	onChange: (value: string) => void;
	className?: string;
}) {
	return (
		<Tabs value={value} onValueChange={onChange}>
			<div className={cn("overflow-x-auto", className)}>
				<TabsList className="h-auto min-w-max justify-start gap-6 rounded-none border-b border-border bg-transparent p-0">
					{options.map((option) => (
						<TabsTrigger
							key={option.value}
							value={option.value}
							className={cn(
								"group relative h-auto cursor-pointer rounded-none px-0 pt-0 pb-4 text-sm font-medium text-muted-foreground transition-colors hover:text-foreground",
								// Base UI Tabs emit `data-active` on the selected trigger;
								// the shared TabsTrigger targets `data-[selected]`, which
								// never matches. Use the real attribute so the underline +
								// active label color render.
								"data-[active]:bg-transparent data-[active]:text-foreground data-[active]:shadow-none",
								"before:pointer-events-none before:absolute before:right-0 before:bottom-[-1px] before:left-0 before:h-0.5 before:rounded-full before:bg-primary before:opacity-0 before:transition-opacity",
								"data-[active]:before:opacity-100",
							)}
						>
							<span>{option.label}</span>
							{option.count !== undefined && (
								<span
									className={cn(
										"ml-2 text-xs font-medium tabular-nums transition-colors",
										option.value === value
											? "text-primary"
											: "text-muted-foreground",
									)}
								>
									{option.count.toLocaleString()}
								</span>
							)}
						</TabsTrigger>
					))}
				</TabsList>
			</div>
		</Tabs>
	);
}
