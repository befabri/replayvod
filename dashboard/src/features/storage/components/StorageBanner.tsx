import { HardDrivesIcon } from "@phosphor-icons/react";
import { Link } from "@tanstack/react-router";
import { useSelector } from "@tanstack/react-store";
import { useTranslation } from "react-i18next";
import {
	storageUnreadable,
	useLiveStorageStatus,
	useStorageDetails,
	useStorageStatus,
} from "@/features/storage/queries";
import { cn } from "@/lib/utils";
import { authStore, hasRole } from "@/stores/auth";

// StorageBanner tells every user why nothing plays or records while storage
// is away, and points owners at the page where they can fix or adopt it. It
// owns the live subscription so the layout only renders it.
export function StorageBanner() {
	const { t } = useTranslation();
	useLiveStorageStatus();
	const { data } = useStorageStatus();
	const isOwner = hasRole(
		useSelector(authStore, (s) => s.user),
		"owner",
	);
	const degraded = data != null && data.state !== "attached";
	const details = useStorageDetails(isOwner && degraded);

	if (!degraded) return null;
	const unreadable = storageUnreadable(data.state);
	const title = unreadable
		? t("storage.banner_unattached_title")
		: data.state === "full"
			? t("storage.banner_full_title")
			: t("storage.banner_read_only_title");
	const body = unreadable
		? t("storage.banner_unattached_body")
		: data.state === "full"
			? t("storage.banner_full_body")
			: t("storage.banner_read_only_body");

	return (
		<div
			data-testid="storage-banner"
			data-state={data.state}
			className={cn(
				"mb-4 flex items-center gap-3 rounded-lg border p-3 text-sm",
				unreadable
					? "border-destructive/40 bg-destructive/10"
					: "border-border bg-muted/50",
			)}
		>
			<HardDrivesIcon
				weight="fill"
				className={cn(
					"size-5 shrink-0",
					unreadable ? "text-destructive" : "text-muted-foreground",
				)}
			/>
			<div className="min-w-0 flex-1">
				<div className="font-medium">{title}</div>
				<div className="text-muted-foreground">{body}</div>
				{isOwner && details.data?.reason ? (
					<div className="mt-1 truncate text-xs text-muted-foreground">
						{t("storage.reason")}: {details.data.reason}
					</div>
				) : null}
			</div>
			{isOwner && (
				<Link
					to="/dashboard/system/storage"
					className="shrink-0 text-sm text-link hover:underline"
				>
					{t("storage.banner_link")}
				</Link>
			)}
		</div>
	);
}
