import { beforeEach, describe, expect, it, vi } from "vitest";
import { getContext } from "@/integrations/tanstack-query/root-provider";
import {
	handleApiError,
	redirectToLogin,
	registerUnauthorizedRedirect,
} from "./unauthorized";

const UNAUTHORIZED = { data: { httpStatus: 401, code: "UNAUTHORIZED" } };

beforeEach(() => {
	// Re-registering re-arms the one-shot guard, giving each test a clean slate.
	registerUnauthorizedRedirect(() => {});
});

describe("handleApiError", () => {
	it("redirects on a 401 httpStatus", () => {
		const redirect = vi.fn();
		registerUnauthorizedRedirect(redirect);
		handleApiError({ data: { httpStatus: 401 } });
		expect(redirect).toHaveBeenCalledTimes(1);
	});

	it("redirects on an UNAUTHORIZED code with no httpStatus", () => {
		const redirect = vi.fn();
		registerUnauthorizedRedirect(redirect);
		handleApiError({ data: { code: "UNAUTHORIZED" } });
		expect(redirect).toHaveBeenCalledTimes(1);
	});

	it("fires once for a burst of 401s (one-shot guard)", () => {
		const redirect = vi.fn();
		registerUnauthorizedRedirect(redirect);
		handleApiError(UNAUTHORIZED);
		handleApiError(UNAUTHORIZED);
		handleApiError(UNAUTHORIZED);
		expect(redirect).toHaveBeenCalledTimes(1);
	});

	it("ignores non-401 errors", () => {
		const redirect = vi.fn();
		registerUnauthorizedRedirect(redirect);
		handleApiError({
			data: { httpStatus: 500, code: "INTERNAL_SERVER_ERROR" },
		});
		handleApiError(new Error("network"));
		handleApiError(null);
		handleApiError(undefined);
		expect(redirect).not.toHaveBeenCalled();
	});

	it("re-arms when a new handler registers (fresh app boot)", () => {
		const first = vi.fn();
		registerUnauthorizedRedirect(first);
		handleApiError(UNAUTHORIZED);
		handleApiError(UNAUTHORIZED);
		expect(first).toHaveBeenCalledTimes(1);

		const second = vi.fn();
		registerUnauthorizedRedirect(second);
		handleApiError(UNAUTHORIZED);
		expect(second).toHaveBeenCalledTimes(1);
	});
});

describe("redirectToLogin", () => {
	it("invokes the registered handler at most once (shared one-shot guard)", () => {
		const redirect = vi.fn();
		registerUnauthorizedRedirect(redirect);
		redirectToLogin();
		redirectToLogin();
		expect(redirect).toHaveBeenCalledTimes(1);
	});
});

// The caches built in getContext() are the global seam that turns any 401 into
// the shared redirect. These assert the wiring, not just the handler.
describe("QueryClient cache wiring", () => {
	it("redirects when a query fails with 401", async () => {
		const redirect = vi.fn();
		registerUnauthorizedRedirect(redirect);
		const { queryClient } = getContext();
		await queryClient
			.fetchQuery({
				queryKey: ["unauthorized-query"],
				queryFn: async () => {
					throw { data: { httpStatus: 401 } };
				},
				retry: false,
			})
			.catch(() => {});
		expect(redirect).toHaveBeenCalledTimes(1);
	});

	it("redirects when a mutation fails with 401", async () => {
		const redirect = vi.fn();
		registerUnauthorizedRedirect(redirect);
		const { queryClient } = getContext();
		const mutation = queryClient.getMutationCache().build(queryClient, {
			mutationFn: async () => {
				throw { data: { code: "UNAUTHORIZED" } };
			},
		});
		await mutation.execute(undefined).catch(() => {});
		expect(redirect).toHaveBeenCalledTimes(1);
	});

	it("leaves an ordinary 500 alone", async () => {
		const redirect = vi.fn();
		registerUnauthorizedRedirect(redirect);
		const { queryClient } = getContext();
		await queryClient
			.fetchQuery({
				queryKey: ["server-error-query"],
				queryFn: async () => {
					throw { data: { httpStatus: 500 } };
				},
				retry: false,
			})
			.catch(() => {});
		expect(redirect).not.toHaveBeenCalled();
	});
});
