import { useState } from "react";
import { useTranslation } from "react-i18next";
import { toast } from "sonner";
import type {
	StorageDetailsResponse,
	StorageState,
} from "@/api/generated/trpc";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import {
	Card,
	CardContent,
	CardDescription,
	CardHeader,
	CardTitle,
} from "@/components/ui/card";
import { ConfirmDialog } from "@/components/ui/confirm-dialog";
import { TimestampValue } from "@/components/ui/timestamp";
import { useAdoptStorage } from "@/features/storage/queries";

const STATE_VARIANT: Record<StorageState, "green" | "yellow" | "red"> = {
	attached: "green",
	read_only: "yellow",
	full: "yellow",
	unattached: "red",
	unreachable: "red",
};

export function StorageCard({ data }: { data: StorageDetailsResponse }) {
	const { t, i18n } = useTranslation();
	const adopt = useAdoptStorage();
	const [confirming, setConfirming] = useState(false);

	const handleAdopt = async () => {
		try {
			const result = await adopt.mutateAsync();
			setConfirming(false);
			if (result.scan_status === "failed") {
				toast.warning(t("storage.adopted_toast_scan_failed"));
			} else {
				toast.success(
					result.scan_status === "scheduled"
						? t("storage.adopted_toast")
						: t("storage.adopted_toast_no_scan"),
				);
			}
		} catch (err) {
			toast.error(
				err instanceof Error ? err.message : t("storage.adopt_failed"),
			);
		}
	};

	return (
		<Card>
			<CardHeader className="sm:flex-row sm:items-start sm:justify-between">
				<div>
					<CardTitle>{t("storage.card_title")}</CardTitle>
					<CardDescription>{t("storage.card_description")}</CardDescription>
				</div>
				<Badge variant={STATE_VARIANT[data.state]} data-testid="storage-state">
					{t(`storage.state_${data.state}`)}
				</Badge>
			</CardHeader>
			<CardContent className="grid gap-4">
				<dl className="grid gap-2 text-sm sm:grid-cols-[max-content_1fr] sm:gap-x-6">
					<dt className="text-muted-foreground">{t("storage.backend")}</dt>
					<dd>{t(`storage.backend_${data.backend}`)}</dd>
					<dt className="text-muted-foreground">{t("storage.location")}</dt>
					<dd className="break-all font-mono text-xs">{data.location}</dd>
					<dt className="text-muted-foreground">{t("storage.storage_id")}</dt>
					<dd>
						<div className="break-all font-mono text-xs">
							{data.storage_id || "—"}
						</div>
						<div className="text-xs text-muted-foreground">
							{t("storage.storage_id_hint")}
						</div>
					</dd>
					<dt className="text-muted-foreground">{t("storage.checked_at")}</dt>
					<dd>
						<TimestampValue iso={data.checked_at} locale={i18n.language} />
					</dd>
					{data.state !== "attached" && data.reason ? (
						<>
							<dt className="text-muted-foreground">{t("storage.reason")}</dt>
							<dd className="break-words">{data.reason}</dd>
						</>
					) : null}
				</dl>
				{data.state === "attached" && (
					<p className="text-sm text-muted-foreground">
						{t("storage.attached_hint")}
					</p>
				)}
				{data.state === "unreachable" && (
					<p className="text-sm text-muted-foreground">
						{t("storage.unreachable_hint")}
					</p>
				)}
				{data.state === "unattached" && (
					<div>
						<Button
							variant="destructive"
							onClick={() => setConfirming(true)}
							disabled={adopt.isPending}
						>
							{t("storage.adopt")}
						</Button>
					</div>
				)}
			</CardContent>
			<ConfirmDialog
				open={confirming}
				onOpenChange={setConfirming}
				title={t("storage.adopt_title")}
				description={t("storage.adopt_description", {
					location: data.location,
				})}
				confirmLabel={t("storage.adopt_confirm")}
				cancelLabel={t("common.cancel")}
				onConfirm={() => void handleAdopt()}
				confirming={adopt.isPending}
				destructive
			/>
		</Card>
	);
}
