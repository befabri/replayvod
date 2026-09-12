// @vitest-environment jsdom

import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import {
	act,
	cleanup,
	fireEvent,
	render,
	screen,
	waitFor,
} from "@testing-library/react";
import { createTRPCClient, httpLink } from "@trpc/client";
import { createElement, type ReactNode, StrictMode } from "react";
import { afterEach, describe, expect, it, vi } from "vitest";
import type {
	LiveRenditionsInput,
	LiveRenditionsResponse,
	Role,
} from "@/api/generated/trpc";
import { type AppRouter, TRPCProvider } from "@/api/trpc";

vi.mock("react-i18next", () => ({
	useTranslation: () => ({ t: (key: string) => key }),
}));
vi.mock("@tanstack/react-router", () => ({
	Link: ({ children, to }: { children?: ReactNode; to: string }) =>
		createElement("a", { href: to }, children),
}));
vi.mock("@/integrations/tanstack-query/root-provider", () => ({
	trpcClient: {},
}));

import { clearUser, setUser } from "@/stores/auth";
import {
	type DirectDownloadController,
	useDirectDownloadForm,
} from "../use-direct-download-form";
import { DirectDownloadFields } from "./DirectDownloadFields";

function json(data: unknown) {
	return new Response(JSON.stringify({ result: { data } }), {
		headers: { "Content-Type": "application/json" },
	});
}
function deferred<T>() {
	let resolve!: (value: T) => void;
	const promise = new Promise<T>((done) => {
		resolve = done;
	});
	return { promise, resolve };
}
const stream: LiveRenditionsResponse = {
	anonymous: false,
	renditions: [
		{ height: 1440, fps: 60, codec: "h265" },
		{ height: 1080, fps: 60, codec: "h264" },
		{ height: 720, fps: 60, codec: "h264" },
		{ height: 480, codec: "h264" },
	],
};

type Props = {
	broadcasterId?: string;
	disabled?: boolean;
};

// Exercise real query observers and form submission. Mocking useQuery flags
// would hide the key changes and cached/paused/error states these tests cover.
function harness(
	fetchRenditions: (
		input: LiveRenditionsInput,
	) => Promise<LiveRenditionsResponse> = async () => stream,
	props: Props = {},
) {
	const requests: LiveRenditionsInput[] = [];
	const queryClient = new QueryClient({
		defaultOptions: { queries: { retry: false, gcTime: 0 } },
	});
	const trpcClient = createTRPCClient<AppRouter>({
		links: [
			httpLink({
				url: "http://example.test/trpc",
				fetch: async (input) => {
					const url = new URL(String(input));
					if (url.pathname.endsWith("twitchPlayback.status"))
						return json({ state: "connected" });
					if (url.pathname.endsWith("stream.isLive")) return json(true);
					const args = JSON.parse(
						url.searchParams.get("input") ?? "{}",
					) as LiveRenditionsInput;
					requests.push(args);
					return json(await fetchRenditions(args));
				},
			}),
		],
	});
	const onSubmit = vi.fn(async () => {});
	let controller!: DirectDownloadController;
	function Harness({ broadcasterId = "b1", ...rest }: Props) {
		controller = useDirectDownloadForm({ broadcasterId, onSubmit, ...rest });
		return (
			<form
				onSubmit={(event) => {
					event.preventDefault();
					void controller.form.handleSubmit();
				}}
			>
				<DirectDownloadFields controller={controller} />
				<button type="submit" disabled={!controller.ready}>
					Submit
				</button>
			</form>
		);
	}
	const wrapper = ({ children }: { children: ReactNode }) => (
		<StrictMode>
			<QueryClientProvider client={queryClient}>
				<TRPCProvider trpcClient={trpcClient} queryClient={queryClient}>
					{children}
				</TRPCProvider>
			</QueryClientProvider>
		</StrictMode>
	);
	const rendered = render(<Harness {...props} />, { wrapper });
	return {
		...rendered,
		queryClient,
		requests,
		onSubmit,
		get controller() {
			return controller;
		},
		rerender: (next: Props) => rendered.rerender(<Harness {...next} />),
		refetch: () => queryClient.invalidateQueries(),
		submit: () =>
			act(async () => {
				await controller.form.handleSubmit();
			}),
	};
}
function picker() {
	return screen.queryByTestId("live-rendition-picker");
}
function selectedHeight(controller: DirectDownloadController) {
	return controller.quality.kind === "rendition"
		? controller.quality.height
		: null;
}
function login(role: Role) {
	setUser({ id: "u", login: "u", displayName: "U", role });
}

afterEach(() => {
	cleanup();
	clearUser();
});

describe("DirectDownloadFields and controller", () => {
	it("blocks initial loading, including direct form submissions", async () => {
		const response = deferred<LiveRenditionsResponse>();
		const view = harness(() => response.promise);
		expect(picker()?.hasAttribute("disabled")).toBe(true);
		expect(view.controller.ready).toBe(false);
		await view.submit();
		expect(view.onSubmit).not.toHaveBeenCalled();
		response.resolve(stream);
		await waitFor(() => expect(view.controller.ready).toBe(true));
		expect(selectedHeight(view.controller)).toBe(1080);
		// Query defaults do not become form values.
		expect(view.controller.values.quality).toBe("HIGH");
		await view.submit();
		expect(view.onSubmit).toHaveBeenCalledWith({
			broadcaster_id: "b1",
			recording_type: "video",
			quality: "HIGH",
			force_h264: false,
			max_height: 1080,
		});
	});

	it("preserves a user-selected ceiling when a failed lookup recovers", async () => {
		let failed = true;
		const view = harness(async () => {
			if (failed) throw new Error("offline");
			return stream;
		});
		await waitFor(() => expect(view.controller.quality.kind).toBe("ceiling"));
		act(() => view.controller.form.setFieldValue("quality", "LOW"));
		failed = false;
		await act(async () => {
			await view.refetch();
		});
		await waitFor(() => expect(selectedHeight(view.controller)).toBe(480));
		await view.submit();
		expect(view.onSubmit).toHaveBeenCalledWith(
			expect.objectContaining({ quality: "LOW", max_height: 480 }),
		);
		expect(view.controller.values.quality).toBe("LOW");
	});

	it("waits for the actual codec query and retains the user's preference", async () => {
		const h264 = deferred<LiveRenditionsResponse>();
		const view = harness(async (input) =>
			input.force_h264
				? h264.promise
				: {
						anonymous: false,
						renditions: [{ height: 1440, codec: "h265" }],
					},
		);
		await waitFor(() => expect(view.controller.quality.kind).toBe("rendition"));
		act(() => view.controller.form.setFieldValue("quality", 1440));
		fireEvent.click(
			screen.getByRole("checkbox", { name: "videos.force_h264" }),
		);
		expect(view.controller.ready).toBe(false);
		expect(picker()?.hasAttribute("disabled")).toBe(true);
		await view.submit();
		expect(view.onSubmit).not.toHaveBeenCalled();
		h264.resolve({
			anonymous: false,
			renditions: [{ height: 1080, codec: "h264" }],
		});
		await waitFor(() => expect(selectedHeight(view.controller)).toBe(1080));
		await view.submit();
		expect(view.onSubmit).toHaveBeenCalledWith(
			expect.objectContaining({ force_h264: true, max_height: 1080 }),
		);
		fireEvent.click(
			screen.getByRole("checkbox", { name: "videos.force_h264" }),
		);
		await waitFor(() => expect(selectedHeight(view.controller)).toBe(1440));
	});

	it("checks the submitted codec even before React renders the changed form", async () => {
		const view = harness(async (input) =>
			input.force_h264 ? new Promise(() => {}) : stream,
		);
		await waitFor(() => expect(view.controller.ready).toBe(true));
		await act(async () => {
			view.controller.form.setFieldValue("force_h264", true);
			await view.controller.form.handleSubmit();
		});
		expect(view.onSubmit).not.toHaveBeenCalled();
	});

	it("does not increase a nonstandard selected height after a playlist refresh", async () => {
		let data: LiveRenditionsResponse = {
			anonymous: false,
			renditions: [
				{ height: 936, codec: "h264" },
				{ height: 720, codec: "h264" },
			],
		};
		const view = harness(async () => data);
		await waitFor(() => expect(view.controller.ready).toBe(true));
		act(() => view.controller.form.setFieldValue("quality", 936));
		data = {
			anonymous: false,
			renditions: [
				{ height: 1080, codec: "h264" },
				{ height: 720, codec: "h264" },
			],
		};
		await act(async () => {
			await view.refetch();
		});
		await waitFor(() => expect(selectedHeight(view.controller)).toBe(720));
		await view.submit();
		expect(view.onSubmit).toHaveBeenLastCalledWith(
			expect.objectContaining({ max_height: 720 }),
		);
		data = { anonymous: false, renditions: [{ height: 1080, codec: "h264" }] };
		await act(async () => {
			await view.refetch();
		});
		await waitFor(() => expect(view.controller.ready).toBe(false));
		expect(
			screen.getByText("videos.download.renditions_no_match"),
		).toBeTruthy();
		view.onSubmit.mockClear();
		await view.submit();
		expect(view.onSubmit).not.toHaveBeenCalled();
		act(() => view.controller.form.setFieldValue("quality", 1080));
		await view.submit();
		expect(view.onSubmit).toHaveBeenCalledWith(
			expect.objectContaining({ max_height: 1080 }),
		);
	});

	it("falls back without a hidden pin after a codec lookup fails", async () => {
		const view = harness(async (input) => {
			if (input.force_h264) throw new Error("no transcodes");
			return stream;
		});
		await waitFor(() => expect(view.controller.ready).toBe(true));
		act(() => view.controller.form.setFieldValue("quality", 720));
		fireEvent.click(
			screen.getByRole("checkbox", { name: "videos.force_h264" }),
		);
		await waitFor(() => expect(view.controller.quality.kind).toBe("ceiling"));
		expect(picker()).toBeNull();
		await view.submit();
		expect(view.onSubmit).toHaveBeenCalledWith({
			broadcaster_id: "b1",
			recording_type: "video",
			quality: "MEDIUM",
			force_h264: true,
		});
	});

	it("allows audio during a pending video lookup and clears the codec", async () => {
		const view = harness(async () => new Promise(() => {}));
		await waitFor(() => expect(view.controller.availability).toBe("live"));
		fireEvent.click(
			screen.getByRole("checkbox", { name: "videos.force_h264" }),
		);
		fireEvent.click(screen.getByRole("radio", { name: "videos.mode_audio" }));
		expect(view.controller.ready).toBe(true);
		expect(picker()).toBeNull();
		expect(
			screen
				.getByRole("checkbox", { name: "videos.force_h264" })
				.getAttribute("aria-checked"),
		).toBe("false");
		await view.submit();
		expect(view.onSubmit).toHaveBeenCalledWith({
			broadcaster_id: "b1",
			recording_type: "audio",
			quality: "HIGH",
			force_h264: false,
		});
	});

	it("blocks a disabled channel without fetching", async () => {
		const view = harness(undefined, { disabled: true });
		expect(view.requests).toEqual([]);
		expect(view.controller.ready).toBe(false);
		expect(
			view.container
				.querySelector('[id$="-quality"]')
				?.hasAttribute("disabled"),
		).toBe(true);
		expect(
			screen
				.getByRole("checkbox", { name: "videos.force_h264" })
				.getAttribute("aria-disabled"),
		).toBe("true");
		await view.submit();
		expect(view.onSubmit).not.toHaveBeenCalled();
	});

	it("does not reuse another broadcaster's list", async () => {
		const next = deferred<LiveRenditionsResponse>();
		const view = harness(async (input) =>
			input.broadcaster_id === "b1" ? stream : next.promise,
		);
		await waitFor(() => expect(view.controller.ready).toBe(true));
		view.rerender({ broadcasterId: "b2" });
		expect(view.controller.ready).toBe(false);
		await view.submit();
		expect(view.onSubmit).not.toHaveBeenCalled();
		next.resolve({
			anonymous: false,
			renditions: [{ height: 720, codec: "h264" }],
		});
		await waitFor(() => expect(view.controller.ready).toBe(true));
		await view.submit();
		expect(view.onSubmit).toHaveBeenCalledWith(
			expect.objectContaining({ broadcaster_id: "b2", max_height: 720 }),
		);
	});

	it.each<[Role, boolean]>([
		["owner", true],
		["admin", false],
	])("explains anonymous access to a %s (link: %s)", async (role, link) => {
		login(role);
		harness(async () => ({ ...stream, anonymous: true }));
		const notice = await screen.findByTestId("renditions-session-notice");
		expect(notice.textContent).toContain(
			`videos.download.renditions_anonymous_${link ? "owner" : "viewer"}`,
		);
		expect(!!screen.queryByRole("link")).toBe(link);
	});

	it("omits the session notice when authenticated renditions are available", async () => {
		login("owner");
		const view = harness();
		await waitFor(() => expect(view.controller.ready).toBe(true));
		expect(screen.queryByTestId("renditions-session-notice")).toBeNull();
	});
});
