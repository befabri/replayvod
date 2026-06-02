import {
	afterAll,
	beforeAll,
	beforeEach,
	describe,
	expect,
	it,
	vi,
} from "vitest";
import { registerUnauthorizedRedirect } from "@/api/unauthorized";

const { sessionQuery, logoutMutate } = vi.hoisted(() => ({
	sessionQuery: vi.fn(),
	logoutMutate: vi.fn(),
}));

// Mock only the transport: the store talks to the vanilla trpcClient, so the
// tests drive session/logout responses without a server.
vi.mock("@/integrations/tanstack-query/root-provider", () => ({
	trpcClient: {
		auth: {
			session: { query: sessionQuery },
			logout: { mutate: logoutMutate },
		},
	},
}));

import {
	authStore,
	clearUser,
	hasRole,
	logout,
	PROBE_COOLDOWN_MS,
	resetSessionCache,
	resolveSession,
	revalidateSession,
	setUser,
	withSessionProbe,
} from "./auth";

const VALID_SESSION = {
	user_id: "u1",
	login: "alice",
	display_name: "Alice",
	email: "alice@example.com",
	profile_image_url: "https://img/alice.png",
	role: "owner",
};

const redirectSpy = vi.fn();

beforeAll(() => {
	// Fake timers with a fixed base make the probe cooldown deterministic.
	vi.useFakeTimers();
	vi.setSystemTime(0);
});

afterAll(() => {
	vi.useRealTimers();
});

beforeEach(() => {
	// Step past the probe cooldown so each test starts able to probe again.
	vi.advanceTimersByTime(PROBE_COOLDOWN_MS + 1_000);
	authStore.setState(() => ({
		isAuthenticated: false,
		user: null,
		isLoading: true,
	}));
	// Clear the module-level session cache so each test resolves from scratch.
	resetSessionCache();
	vi.clearAllMocks();
	// Re-registering re-arms the shared 401 one-shot guard for each test.
	registerUnauthorizedRedirect(redirectSpy);
});

describe("logout", () => {
	it("calls the server then clears local auth", async () => {
		setUser({ id: "u1", login: "alice", displayName: "Alice", role: "owner" });
		logoutMutate.mockResolvedValue(undefined);

		await logout();

		expect(logoutMutate).toHaveBeenCalledTimes(1);
		expect(authStore.state.isAuthenticated).toBe(false);
		expect(authStore.state.user).toBeNull();
	});

	it("clears local auth even when the server call fails", async () => {
		setUser({ id: "u1", login: "alice", displayName: "Alice", role: "owner" });
		logoutMutate.mockRejectedValue(new Error("network"));

		await logout();

		expect(authStore.state.isAuthenticated).toBe(false);
		expect(authStore.state.user).toBeNull();
	});
});

describe("resolveSession", () => {
	it("resolves a valid session and returns the user without mutating the store", async () => {
		sessionQuery.mockResolvedValue(VALID_SESSION);

		const user = await resolveSession();

		expect(user?.login).toBe("alice");
		expect(user?.role).toBe("owner");
		// Pure: the protected layout hydrates the store after mount, not the guard.
		expect(authStore.state.user).toBeNull();
		expect(authStore.state.isAuthenticated).toBe(false);
	});

	it("returns null on a 401", async () => {
		sessionQuery.mockRejectedValue({ data: { httpStatus: 401 } });

		const user = await resolveSession();

		expect(user).toBeNull();
	});

	it("treats an unknown role as no session", async () => {
		sessionQuery.mockResolvedValue({ ...VALID_SESSION, role: "superadmin" });

		const user = await resolveSession();

		expect(user).toBeNull();
	});

	it("dedupes concurrent calls into a single request", async () => {
		let resolve: (v: unknown) => void = () => {};
		sessionQuery.mockReturnValue(
			new Promise((r) => {
				resolve = r;
			}),
		);

		const p1 = resolveSession();
		const p2 = resolveSession();
		resolve(VALID_SESSION);
		await Promise.all([p1, p2]);

		expect(sessionQuery).toHaveBeenCalledTimes(1);
	});

	it("caches the result and does not refetch on later calls", async () => {
		sessionQuery.mockResolvedValue(VALID_SESSION);

		const first = await resolveSession();
		const second = await resolveSession();

		expect(first?.login).toBe("alice");
		expect(second?.login).toBe("alice");
		expect(sessionQuery).toHaveBeenCalledTimes(1);
	});

	it("returns null without refetching once the session was cleared (logout)", async () => {
		setUser({ id: "u1", login: "alice", displayName: "Alice", role: "owner" });
		await resolveSession();
		expect(sessionQuery).not.toHaveBeenCalled();

		// After logout the cache holds "no session": still no refetch, returns null.
		clearUser();
		const user = await resolveSession();

		expect(user).toBeNull();
		expect(sessionQuery).not.toHaveBeenCalled();
	});

	it("does not itself redirect on a 401 (the route guard throws redirect)", async () => {
		sessionQuery.mockRejectedValue({ data: { httpStatus: 401 } });

		await resolveSession();

		expect(redirectSpy).not.toHaveBeenCalled();
	});
});

describe("revalidateSession (SSE probe)", () => {
	it("redirects when the probe returns 401", async () => {
		sessionQuery.mockRejectedValue({ data: { httpStatus: 401 } });

		await revalidateSession();

		expect(redirectSpy).toHaveBeenCalledTimes(1);
	});

	it("does not redirect on a transient (non-401) failure", async () => {
		sessionQuery.mockRejectedValue(new Error("connection reset"));

		await revalidateSession();

		expect(redirectSpy).not.toHaveBeenCalled();
	});

	it("refreshes the store and does not redirect when still valid", async () => {
		sessionQuery.mockResolvedValue(VALID_SESSION);

		await revalidateSession();

		expect(authStore.state.isAuthenticated).toBe(true);
		expect(redirectSpy).not.toHaveBeenCalled();
	});

	it("shares one in-flight probe across concurrent stream errors", async () => {
		let resolve: (v: unknown) => void = () => {};
		sessionQuery.mockReturnValue(
			new Promise((r) => {
				resolve = r;
			}),
		);

		const a = revalidateSession();
		const b = revalidateSession();
		const c = revalidateSession();
		resolve(VALID_SESSION);
		await Promise.all([a, b, c]);

		expect(sessionQuery).toHaveBeenCalledTimes(1);
	});

	it("redirects when the probe returns a 200 with an unusable role", async () => {
		sessionQuery.mockResolvedValue({ ...VALID_SESSION, role: "superadmin" });

		await revalidateSession();

		expect(redirectSpy).toHaveBeenCalledTimes(1);
	});

	it("skips re-probing within the cooldown after a confirmed session", async () => {
		sessionQuery.mockResolvedValue(VALID_SESSION);

		await revalidateSession();
		await revalidateSession();
		expect(sessionQuery).toHaveBeenCalledTimes(1);

		vi.advanceTimersByTime(PROBE_COOLDOWN_MS + 1);
		await revalidateSession();
		expect(sessionQuery).toHaveBeenCalledTimes(2);
	});
});

describe("withSessionProbe", () => {
	it("fires the probe and forwards to the wrapped handler", async () => {
		sessionQuery.mockResolvedValue(VALID_SESSION);
		const inner = vi.fn();

		const onError = withSessionProbe(inner);
		onError("stream dropped");

		expect(inner).toHaveBeenCalledWith("stream dropped");
		// Awaiting the shared in-flight probe settles it before the next test.
		await revalidateSession();
		expect(sessionQuery).toHaveBeenCalledTimes(1);
	});

	it("works without a wrapped handler", async () => {
		sessionQuery.mockResolvedValue(VALID_SESSION);

		const onError = withSessionProbe();
		expect(() => onError("x")).not.toThrow();

		await revalidateSession();
		expect(sessionQuery).toHaveBeenCalledTimes(1);
	});
});

describe("hasRole", () => {
	it("respects the role hierarchy", () => {
		const owner = {
			id: "1",
			login: "o",
			displayName: "O",
			role: "owner",
		} as const;
		const viewer = {
			id: "2",
			login: "v",
			displayName: "V",
			role: "viewer",
		} as const;
		expect(hasRole(owner, "admin")).toBe(true);
		expect(hasRole(viewer, "admin")).toBe(false);
		expect(hasRole(null, "viewer")).toBe(false);
	});
});
