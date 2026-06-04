// @vitest-environment jsdom

import {
	cleanup,
	fireEvent,
	render,
	screen,
	waitFor,
} from "@testing-library/react";
import { createElement } from "react";
import { afterEach, describe, expect, it, vi } from "vitest";
import type { ConfigResponse, UpdateConfigInput } from "@/api/generated/trpc";
import { EventSubSetupCard, payload } from "./EventSubSetupCard";

const updateMock = vi.hoisted(() => ({
	mutate: vi.fn(),
	mutateAsync: vi.fn(),
	data: undefined as unknown,
}));

vi.mock("react-i18next", () => ({
	useTranslation: () => ({
		t: (key: string) => key,
	}),
}));

vi.mock("../queries", () => ({
	useUpdateEventSubConfig: () => ({
		mutate: updateMock.mutate,
		mutateAsync: updateMock.mutateAsync,
		isPending: false,
		isError: false,
		isSuccess: false,
		data: updateMock.data,
		error: undefined,
	}),
}));

afterEach(() => {
	cleanup();
	updateMock.mutate.mockClear();
	updateMock.mutateAsync.mockClear();
	updateMock.data = undefined;
});

// payload sends only the URL the owner actually types: the relay URL for relay,
// nothing for off/poll/direct (the server derives the rest). `filled` carries a
// stale URL from every field so these cases pin that nothing foreign leaks into
// the saved config.
const filled: UpdateConfigInput = {
	mode: "relay",
	webhook_callback_url: "  https://replayvod.example/api/v1/webhook/callback  ",
	relay_ingest_url: "  https://relay.replayvod.com/u/token  ",
	relay_subscribe_url: "  wss://relay.replayvod.com/u/token/subscribe  ",
	relay_local_callback_url: "  http://127.0.0.1:8080/api/v1/webhook/callback  ",
};

function configResponse(
	overrides: Partial<ConfigResponse> = {},
): ConfigResponse {
	return {
		source: "app",
		mode: "relay",
		env_managed: false,
		setup_required: false,
		restart_required: false,
		creates_twitch_subscriptions: true,
		uses_relay_agent: true,
		polls_helix: false,
		relay_ingest_url: "https://relay.replayvod.com/u/token",
		relay_subscribe_url: "wss://relay.replayvod.com/u/token/subscribe",
		relay_local_callback_url: "http://127.0.0.1:8080/api/v1/webhook/callback",
		direct_callback_url: "https://replayvod.example/api/v1/webhook/callback",
		active: {
			source: "app",
			mode: "relay",
			creates_twitch_subscriptions: true,
			uses_relay_agent: true,
			polls_helix: false,
		},
		...overrides,
	};
}

describe("EventSubSetupCard payload", () => {
	it("off sends only the mode", () => {
		expect(payload({ ...filled, mode: "off" })).toEqual({ mode: "off" });
	});

	it("poll sends only the mode", () => {
		expect(payload({ ...filled, mode: "poll" })).toEqual({ mode: "poll" });
	});

	it("direct sends only the mode; the server derives the callback", () => {
		expect(payload({ ...filled, mode: "direct" })).toEqual({ mode: "direct" });
	});

	it("relay sends only the trimmed relay URL", () => {
		expect(payload({ ...filled, mode: "relay" })).toEqual({
			mode: "relay",
			relay_ingest_url: "https://relay.replayvod.com/u/token",
		});
	});
});

describe("EventSubSetupCard rendered form", () => {
	it("submits the edited relay URL through the mutation payload", async () => {
		updateMock.mutateAsync.mockResolvedValue(configResponse());
		render(createElement(EventSubSetupCard, { data: configResponse() }));

		fireEvent.change(screen.getByLabelText("eventsub.relay_url"), {
			target: { value: " https://relay.replayvod.com/u/edited " },
		});

		fireEvent.click(
			screen.getByRole("button", { name: "eventsub.save_config" }),
		);

		await waitFor(() => {
			expect(updateMock.mutateAsync).toHaveBeenCalledWith({
				mode: "relay",
				relay_ingest_url: "https://relay.replayvod.com/u/edited",
			});
		});
	});

	it("shows the derived direct callback read-only and submits only the mode", async () => {
		const directConfig = configResponse({
			mode: "direct",
			uses_relay_agent: false,
			relay_ingest_url: undefined,
			relay_subscribe_url: undefined,
			relay_local_callback_url: undefined,
			direct_callback_url: "https://replayvod.example/api/v1/webhook/callback",
			active: {
				source: "app",
				mode: "direct",
				creates_twitch_subscriptions: true,
				uses_relay_agent: false,
				polls_helix: false,
			},
		});
		updateMock.mutateAsync.mockResolvedValue(directConfig);
		render(createElement(EventSubSetupCard, { data: directConfig }));

		// The derived callback is shown; there is no editable webhook field.
		expect(
			screen.getByText("https://replayvod.example/api/v1/webhook/callback"),
		).toBeTruthy();
		expect(screen.queryByLabelText("eventsub.webhook_callback_url")).toBeNull();

		fireEvent.click(
			screen.getByRole("button", { name: "eventsub.save_config" }),
		);

		await waitFor(() => {
			expect(updateMock.mutateAsync).toHaveBeenCalledWith({ mode: "direct" });
		});
	});

	it("prompts for a public URL when direct mode has no derived callback", () => {
		const directConfig = configResponse({
			mode: "direct",
			uses_relay_agent: false,
			relay_ingest_url: undefined,
			relay_subscribe_url: undefined,
			relay_local_callback_url: undefined,
			direct_callback_url: undefined,
			active: {
				source: "app",
				mode: "direct",
				creates_twitch_subscriptions: true,
				uses_relay_agent: false,
				polls_helix: false,
			},
		});
		render(createElement(EventSubSetupCard, { data: directConfig }));

		expect(screen.getByText("eventsub.direct_needs_public_url")).toBeTruthy();
	});

	it("keeps unsaved edits for a retry when the save fails", async () => {
		updateMock.mutateAsync.mockRejectedValueOnce(new Error("save failed"));
		render(createElement(EventSubSetupCard, { data: configResponse() }));

		const relay = screen.getByLabelText(
			"eventsub.relay_url",
		) as HTMLInputElement;
		fireEvent.change(relay, {
			target: { value: "https://relay.replayvod.com/u/edited" },
		});
		fireEvent.click(
			screen.getByRole("button", { name: "eventsub.save_config" }),
		);

		await waitFor(() => {
			expect(updateMock.mutateAsync).toHaveBeenCalled();
		});
		// The rejection is swallowed (no reset to server values), so the edit
		// survives. A leaked rejection would fail this test.
		expect(relay.value).toBe("https://relay.replayvod.com/u/edited");
	});

	it("reads status from the fresh data prop, not a stale mutation response", () => {
		// Simulate the post-restart window: a prior save left a sticky mutation
		// response saying "restart required, running poll", but the config query
		// has since refetched and reports the restarted, active relay config.
		updateMock.data = configResponse({
			mode: "poll",
			restart_required: true,
			active: {
				source: "app",
				mode: "poll",
				creates_twitch_subscriptions: false,
				uses_relay_agent: false,
				polls_helix: true,
			},
		});
		render(createElement(EventSubSetupCard, { data: configResponse() }));

		// The badge must reflect the fresh data prop (active), not the sticky
		// "restart required" mutation response.
		expect(screen.getByText("eventsub.active")).toBeTruthy();
		expect(screen.queryByText("eventsub.restart_required")).toBeNull();
	});

	it("preserves unsaved edits during a config refetch", () => {
		const { rerender } = render(
			createElement(EventSubSetupCard, { data: configResponse() }),
		);

		const relay = screen.getByLabelText(
			"eventsub.relay_url",
		) as HTMLInputElement;
		fireEvent.change(relay, {
			target: { value: "https://relay.replayvod.com/u/unsaved" },
		});

		rerender(createElement(EventSubSetupCard, { data: configResponse() }));

		expect(relay.value).toBe("https://relay.replayvod.com/u/unsaved");
	});
});
