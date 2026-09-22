import { useRouter, useRouterState } from "@tanstack/react-router";
import { useTranslation } from "react-i18next";
import { Button } from "@/components/ui/button";

export function RouteError() {
	const router = useRouter();
	const isLoading = useRouterState({ select: (state) => state.isLoading });
	const { t } = useTranslation();

	return (
		<main className="flex min-h-screen items-center justify-center p-6">
			<div role="alert" className="max-w-md space-y-4 text-center">
				<h1 className="font-heading text-xl font-semibold">
					{t("common.page_load_failed")}
				</h1>
				<p className="text-muted-foreground">{t("common.page_load_retry")}</p>
				<Button disabled={isLoading} onClick={() => void router.invalidate()}>
					{t(isLoading ? "common.loading" : "common.retry")}
				</Button>
			</div>
		</main>
	);
}
