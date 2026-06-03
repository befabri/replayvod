import { ArrowRightIcon, WarningCircleIcon } from "@phosphor-icons/react";
import { Link } from "@tanstack/react-router";
import { useTranslation } from "react-i18next";
import { buttonVariants } from "@/components/ui/button";
import {
	Card,
	CardDescription,
	CardHeader,
	CardTitle,
} from "@/components/ui/card";

// EventSubSetupNudge is the dashboard-home prompt: a compact alert linking to
// the full setup form on the system page. The form lives in exactly one place
// (EventSubSetupCard on /dashboard/system/eventsub), so its save/success/restart
// feedback is never lost when this prompt unmounts after a successful save.
export function EventSubSetupNudge({
	setupRequired,
	restartRequired,
}: {
	setupRequired: boolean;
	restartRequired: boolean;
}) {
	const { t } = useTranslation();
	const needsRestart = restartRequired && !setupRequired;

	return (
		<Card>
			<CardHeader className="sm:flex-row sm:items-center sm:justify-between">
				<div className="flex items-start gap-2">
					<WarningCircleIcon className="size-5 shrink-0 text-yellow-600" />
					<div>
						<CardTitle>
							{needsRestart
								? t("eventsub.restart_required")
								: t("eventsub.setup_required")}
						</CardTitle>
						<CardDescription>
							{needsRestart
								? t("eventsub.nudge_restart")
								: t("eventsub.nudge_setup")}
						</CardDescription>
					</div>
				</div>
				<Link
					to="/dashboard/system/eventsub"
					className={buttonVariants({ variant: "outline" })}
				>
					{t("eventsub.configure")}
					<ArrowRightIcon data-icon="inline-end" />
				</Link>
			</CardHeader>
		</Card>
	);
}
