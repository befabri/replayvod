import { createFileRoute } from "@tanstack/react-router";
import { useTranslation } from "react-i18next";
import { TitledLayout } from "@/components/layout/titled-layout";
import { Alert } from "@/components/ui/alert";
import {
	StorageCard,
	StorageCardSkeleton,
} from "@/features/storage/components/StorageCard";
import { useStorageDetails } from "@/features/storage/queries";
import { requireRole } from "@/lib/route-guards";

export const Route = createFileRoute("/dashboard/system/storage")({
	beforeLoad: requireRole("owner"),
	component: StoragePage,
});

function StoragePage() {
	const { t } = useTranslation();
	const details = useStorageDetails();

	return (
		<TitledLayout title={t("storage.title")}>
			<p className="text-muted-foreground mb-6 -mt-6">
				{t("storage.page_description")}
			</p>
			{details.isLoading && (
				<div className="grid gap-6">
					<StorageCardSkeleton />
				</div>
			)}
			{details.isError && (
				<Alert variant="destructive">
					{details.error?.message ?? t("storage.load_failed")}
				</Alert>
			)}
			{details.data && (
				<div className="grid gap-6">
					<StorageCard data={details.data} />
				</div>
			)}
		</TitledLayout>
	);
}
