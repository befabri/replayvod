import { useId } from "react";
import { useTranslation } from "react-i18next";
import { Button } from "@/components/ui/button";
import {
	Tooltip,
	TooltipContent,
	TooltipProvider,
	TooltipTrigger,
} from "@/components/ui/tooltip";
import type { TaskResponse } from "@/features/tasks";
import { useRunTaskNow, useToggleTask } from "@/features/tasks";

export function TaskActions({ task }: { task: TaskResponse }) {
	const { t } = useTranslation();
	const toggle = useToggleTask();
	const runNow = useRunTaskNow();
	const reasonId = useId();
	const unavailable = !task.is_available;
	return (
		<div className="flex flex-col items-end gap-1">
			<TooltipProvider>
				<Tooltip disabled={!unavailable}>
					<TooltipTrigger
						render={
							<Button
								onClick={() => runNow.mutate({ name: task.name })}
								disabled={runNow.isPending || unavailable}
								focusableWhenDisabled={unavailable}
								aria-describedby={unavailable ? reasonId : undefined}
								variant="outline"
								size="xs"
							>
								{t("tasks.run_now")}
							</Button>
						}
					/>
					<TooltipContent>{t("tasks.unavailable")}</TooltipContent>
				</Tooltip>
			</TooltipProvider>
			{unavailable ? (
				<span id={reasonId} className="sr-only">
					{t("tasks.unavailable")}
				</span>
			) : null}
			<Button
				onClick={() =>
					toggle.mutate({ name: task.name, enabled: !task.is_enabled })
				}
				disabled={toggle.isPending}
				variant="ghost"
				size="xs"
			>
				{task.is_enabled ? t("tasks.pause") : t("tasks.resume")}
			</Button>
		</div>
	);
}
