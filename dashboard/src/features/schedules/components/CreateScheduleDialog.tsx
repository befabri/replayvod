import { PlusIcon } from "@phosphor-icons/react";
import { useState } from "react";
import { useTranslation } from "react-i18next";
import { Button } from "@/components/ui/button";
import {
	Dialog,
	DialogContent,
	DialogHeader,
	DialogTitle,
	DialogTrigger,
} from "@/components/ui/dialog";
import { CreateForm } from "./CreateForm";

// CreateScheduleDialog is the schedules page's "New schedule" entry point:
// a trigger button that opens the creation form in a modal, so the page
// itself is just the list. The form is mounted only while open so each run
// starts from clean defaults. An optional custom trigger lets callers reuse
// this from places like the empty state.
export function CreateScheduleDialog({
	trigger,
}: {
	trigger?: React.ReactElement;
}) {
	const { t } = useTranslation();
	const [open, setOpen] = useState(false);

	return (
		<Dialog open={open} onOpenChange={setOpen}>
			<DialogTrigger
				render={
					trigger ?? (
						<Button>
							<PlusIcon weight="bold" />
							{t("schedules.create_title")}
						</Button>
					)
				}
			/>
			{open && (
				<DialogContent className="max-w-xl">
					<DialogHeader>
						<DialogTitle>{t("schedules.create_title")}</DialogTitle>
					</DialogHeader>
					<CreateForm onDone={() => setOpen(false)} />
				</DialogContent>
			)}
		</Dialog>
	);
}
