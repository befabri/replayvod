import { QueryErrorResetBoundary, useQueryClient } from "@tanstack/react-query";
import { CatchBoundary } from "@tanstack/react-router";
import { isTRPCClientError } from "@trpc/client";
import {
	type ReactNode,
	Suspense,
	useEffect,
	useEffectEvent,
	useMemo,
	useState,
} from "react";
import { useTranslation } from "react-i18next";
import { Alert } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { queryErrorLabel } from "@/lib/query";

export function QueryBoundary({
	fallback,
	children,
}: {
	fallback: ReactNode;
	children: ReactNode;
}) {
	const [mountedAt] = useState(Date.now);
	return (
		<QueryErrorResetBoundary>
			{({ reset }) => (
				<CatchBoundary
					getResetKey={() => 0}
					errorComponent={({ error, reset: retry }) => (
						<QueryBoundaryError
							error={error}
							boundaryMountedAt={mountedAt}
							onRetry={() => {
								reset();
								retry();
							}}
						/>
					)}
				>
					<Suspense fallback={fallback}>{children}</Suspense>
				</CatchBoundary>
			)}
		</QueryErrorResetBoundary>
	);
}

function QueryBoundaryError({
	error,
	boundaryMountedAt,
	onRetry,
}: {
	error: unknown;
	boundaryMountedAt: number;
	onRetry: () => void;
}) {
	const { t } = useTranslation();
	const queryClient = useQueryClient();
	const query = useMemo(
		() =>
			queryClient
				.getQueryCache()
				.getAll()
				.find((candidate) => candidate.state.error === error),
		[queryClient, error],
	);
	const recover = useEffectEvent(onRetry);

	useEffect(() => {
		if (!query) return;
		const unsubscribe = queryClient.getQueryCache().subscribe((event) => {
			if (
				event.type === "updated" &&
				event.action.type === "success" &&
				event.query === query
			) {
				recover();
			}
		});
		if (
			query.state.fetchStatus === "idle" &&
			query.state.errorUpdatedAt < boundaryMountedAt
		) {
			void queryClient.refetchQueries({
				queryKey: query.queryKey,
				exact: true,
			});
		}
		return unsubscribe;
	}, [boundaryMountedAt, query, queryClient]);

	if (!isTRPCClientError(error)) throw error;
	return (
		<Alert
			variant="destructive"
			action={
				<Button variant="outline" size="sm" onClick={onRetry}>
					{t("common.retry")}
				</Button>
			}
		>
			{t(queryErrorLabel(query))}: {error.message}
		</Alert>
	);
}
