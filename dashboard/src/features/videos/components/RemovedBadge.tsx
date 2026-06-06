import type { TFunction } from "i18next";
import { Badge } from "@/components/ui/badge";

// RemovedBadge marks a tombstoned (deleted) recording in the history audit log.
// "removed" is a deleted_at state, not a VideoStatus value, so it renders as a
// secondary badge next to VideoStatusBadge (mirroring the PARTIAL secondary
// badge) rather than as a status. deletionKind tells auto-cleanup (retention)
// apart from an operator delete (manual).
export function RemovedBadge({
	deletionKind,
	t,
}: {
	deletionKind?: string | null;
	t: TFunction;
}) {
	const label =
		deletionKind === "retention"
			? t("history.removed_auto")
			: t("history.removed_manual");
	return <Badge variant="muted">{label}</Badge>;
}
