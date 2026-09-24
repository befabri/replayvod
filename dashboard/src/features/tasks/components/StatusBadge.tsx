import { useTranslation } from "react-i18next";
import { Badge } from "@/components/ui/badge";
import { cn } from "@/lib/utils";

const STATUS_VARIANTS = {
	success: "green",
	failed: "red",
	running: "blue",
} as const;

export function StatusBadge({
	status,
	enabled,
	error,
}: {
	status: string;
	enabled: boolean;
	error?: string;
}) {
	const { t } = useTranslation();
	if (!enabled) {
		return <Badge variant="muted">{t("tasks.status_paused")}</Badge>;
	}
	const variant =
		STATUS_VARIANTS[status as keyof typeof STATUS_VARIANTS] ?? "muted";
	return (
		<>
			<Badge
				variant={variant}
				className={cn(status === "running" && "animate-pulse")}
			>
				{t(`tasks.status_${status}`, { defaultValue: status })}
			</Badge>
			{error && (
				<div
					className="text-xs text-destructive mt-1 max-w-sm truncate"
					title={error}
				>
					{error}
				</div>
			)}
		</>
	);
}
