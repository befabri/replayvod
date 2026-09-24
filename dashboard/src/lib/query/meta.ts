import type { Query } from "@tanstack/react-query";

declare module "@tanstack/react-query" {
	interface Register {
		queryMeta: {
			errorLabel?: string;
		};
	}
}

export const DEFAULT_QUERY_ERROR_LABEL = "common.failed_to_load";

export function queryErrorLabel(query: Query | undefined): string {
	return query?.meta?.errorLabel ?? DEFAULT_QUERY_ERROR_LABEL;
}
