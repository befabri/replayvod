import {
	ArrowRightIcon,
	BroadcastIcon,
	WarningCircleIcon,
} from "@phosphor-icons/react";
import { Link } from "@tanstack/react-router";
import { useTranslation } from "react-i18next";
import { buttonVariants } from "@/components/ui/button";
import { cn } from "@/lib/utils";

// EventSubSetupNudge is the dashboard-home prompt: a compact onboarding callout
// linking to the full setup form on the system page. The form lives in exactly
// one place (EventSubSetupCard on /dashboard/system/eventsub), so its
// save/success/restart feedback is never lost when this prompt unmounts after a
// successful save.
//
// Two states share the layout: the first-run "set up live detection" nudge
// (primary accent, inviting) and the post-save "restart required" reminder
// (amber accent, advisory).
export function EventSubSetupNudge({
	setupRequired,
	restartRequired,
}: {
	setupRequired: boolean;
	restartRequired: boolean;
}) {
	const { t } = useTranslation();
	const needsRestart = restartRequired && !setupRequired;

	const Icon = needsRestart ? WarningCircleIcon : BroadcastIcon;
	const title = needsRestart
		? t("eventsub.restart_required")
		: t("eventsub.nudge_title");
	const body = needsRestart
		? t("eventsub.nudge_restart")
		: t("eventsub.nudge_setup");

	return (
		<div className="rounded-xl border border-border bg-card shadow-sm">
			<div className="flex flex-col gap-4 p-5 sm:flex-row sm:items-center sm:justify-between">
				<div className="flex items-start gap-4">
					<span
						className={cn(
							"flex size-11 shrink-0 items-center justify-center rounded-lg",
							needsRestart
								? "bg-yellow-500/10 text-yellow-500"
								: "bg-primary/10 text-primary",
						)}
					>
						<Icon className="size-6" />
					</span>
					<div className="space-y-1">
						<h3 className="text-base font-semibold leading-tight text-foreground">
							{title}
						</h3>
						<p className="max-w-prose text-sm text-muted-foreground">{body}</p>
					</div>
				</div>
				<Link
					to="/dashboard/system/eventsub"
					className={cn(
						buttonVariants({ variant: needsRestart ? "outline" : "default" }),
						"shrink-0 self-start sm:self-auto",
					)}
				>
					{t("eventsub.configure")}
					<ArrowRightIcon data-icon="inline-end" />
				</Link>
			</div>
		</div>
	);
}
