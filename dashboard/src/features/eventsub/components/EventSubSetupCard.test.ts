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

// payload must send only the fields the chosen mode uses, trimmed,
// mirroring the server's ClearURLsForDelivery. These cases pin that a stale URL
// left in the form from another mode never leaks into the saved config.
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
	it("off clears every URL", () => {
		expect(payload({ ...filled, mode: "off" })).toEqual({
			mode: "off",
			webhook_callback_url: "",
			relay_ingest_url: "",
			relay_subscribe_url: "",
			relay_local_callback_url: "",
		});
	});

	it("poll clears every URL", () => {
		expect(payload({ ...filled, mode: "poll" })).toEqual({
			mode: "poll",
			webhook_callback_url: "",
			relay_ingest_url: "",
			relay_subscribe_url: "",
			relay_local_callback_url: "",
		});
	});

	it("direct keeps only the trimmed webhook URL", () => {
		expect(payload({ ...filled, mode: "direct" })).toEqual({
			mode: "direct",
			webhook_callback_url: "https://replayvod.example/api/v1/webhook/callback",
			relay_ingest_url: "",
			relay_subscribe_url: "",
			relay_local_callback_url: "",
		});
	});

	it("relay keeps only the trimmed relay URLs", () => {
		expect(payload({ ...filled, mode: "relay" })).toEqual({
			mode: "relay",
			webhook_callback_url: "",
			relay_ingest_url: "https://relay.replayvod.com/u/token",
			relay_subscribe_url: "wss://relay.replayvod.com/u/token/subscribe",
			relay_local_callback_url: "http://127.0.0.1:8080/api/v1/webhook/callback",
		});
	});
});

describe("EventSubSetupCard rendered form", () => {
	it("submits edited relay fields through the mutation payload", async () => {
		updateMock.mutateAsync.mockResolvedValue(configResponse());
		render(createElement(EventSubSetupCard, { data: configResponse() }));

		fireEvent.change(screen.getByLabelText("eventsub.relay_ingest_url"), {
			target: { value: " https://relay.replayvod.com/u/edited " },
		});
		fireEvent.change(screen.getByLabelText("eventsub.relay_subscribe_url"), {
			target: { value: " wss://relay.replayvod.com/u/edited/subscribe " },
		});
		fireEvent.change(
			screen.getByLabelText("eventsub.relay_local_callback_url"),
			{
				target: {
					value: " http://127.0.0.1:9090/api/v1/webhook/callback ",
				},
			},
		);

		fireEvent.click(
			screen.getByRole("button", { name: "eventsub.save_config" }),
		);

		await waitFor(() => {
			expect(updateMock.mutateAsync).toHaveBeenCalledWith({
				mode: "relay",
				webhook_callback_url: "",
				relay_ingest_url: "https://relay.replayvod.com/u/edited",
				relay_subscribe_url: "wss://relay.replayvod.com/u/edited/subscribe",
				relay_local_callback_url:
					"http://127.0.0.1:9090/api/v1/webhook/callback",
			});
		});
	});

	it("submits the direct webhook field through the mutation payload", async () => {
		const directConfig = configResponse({
			mode: "direct",
			uses_relay_agent: false,
			relay_ingest_url: undefined,
			relay_subscribe_url: undefined,
			relay_local_callback_url: undefined,
			webhook_callback_url: "https://replayvod.example/api/v1/webhook/callback",
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

		fireEvent.change(screen.getByLabelText("eventsub.webhook_callback_url"), {
			target: { value: " https://new.example/api/v1/webhook/callback " },
		});
		fireEvent.click(
			screen.getByRole("button", { name: "eventsub.save_config" }),
		);

		await waitFor(() => {
			expect(updateMock.mutateAsync).toHaveBeenCalledWith({
				mode: "direct",
				webhook_callback_url: "https://new.example/api/v1/webhook/callback",
				relay_ingest_url: "",
				relay_subscribe_url: "",
				relay_local_callback_url: "",
			});
		});
	});

	it("keeps unsaved edits for a retry when the save fails", async () => {
		updateMock.mutateAsync.mockRejectedValueOnce(new Error("save failed"));
		render(createElement(EventSubSetupCard, { data: configResponse() }));

		const ingest = screen.getByLabelText(
			"eventsub.relay_ingest_url",
		) as HTMLInputElement;
		fireEvent.change(ingest, {
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
		expect(ingest.value).toBe("https://relay.replayvod.com/u/edited");
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

		const ingest = screen.getByLabelText(
			"eventsub.relay_ingest_url",
		) as HTMLInputElement;
		fireEvent.change(ingest, {
			target: { value: "https://relay.replayvod.com/u/unsaved" },
		});

		rerender(createElement(EventSubSetupCard, { data: configResponse() }));

		expect(ingest.value).toBe("https://relay.replayvod.com/u/unsaved");
	});
});
