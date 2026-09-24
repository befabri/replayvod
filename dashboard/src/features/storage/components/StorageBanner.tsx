import { HardDrivesIcon } from "@phosphor-icons/react";
import { Link } from "@tanstack/react-router";
import { useSelector } from "@tanstack/react-store";
import { useTranslation } from "react-i18next";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import { buttonVariants } from "@/components/ui/button";
import {
	storageUnreadable,
	useLiveStorageStatus,
	useStorageDetails,
	useStorageStatus,
} from "@/features/storage/queries";
import { authStore, hasRole } from "@/stores/auth";

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
		<Alert
			data-testid="storage-banner"
			data-state={data.state}
			variant={unreadable ? "destructive" : "default"}
			className="mb-4"
			icon={<HardDrivesIcon weight="fill" />}
			action={
				isOwner ? (
					<Link
						to="/dashboard/system/storage"
						className={buttonVariants({ variant: "link", size: "inline" })}
					>
						{t("storage.banner_link")}
					</Link>
				) : null
			}
		>
			<AlertTitle>{title}</AlertTitle>
			<AlertDescription>{body}</AlertDescription>
			{isOwner && details.data?.reason ? (
				<AlertDescription className="mt-1 truncate text-xs">
					{t("storage.reason")}: {details.data.reason}
				</AlertDescription>
			) : null}
		</Alert>
	);
}
