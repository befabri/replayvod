// @vitest-environment jsdom

import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { createElement } from "react";
import { afterEach, describe, expect, it, vi } from "vitest";
import type { ConfigResponse, UpdateConfigInput } from "@/api/generated/trpc";
import {
	EventSubSetupCard,
	formValuesAfterConfigRefresh,
	formValuesDirty,
	payload,
} from "./EventSubSetupCard";

const updateMock = vi.hoisted(() => ({
	mutate: vi.fn(),
}));

vi.mock("react-i18next", () => ({
	useTranslation: () => ({
		t: (key: string) => key,
	}),
}));

vi.mock("../queries", () => ({
	useUpdateEventSubConfig: () => ({
		mutate: updateMock.mutate,
		isPending: false,
		isError: false,
		isSuccess: false,
		data: undefined,
		error: undefined,
	}),
}));

afterEach(() => {
	cleanup();
	updateMock.mutate.mockClear();
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

describe("EventSubSetupCard config refresh", () => {
	it("rehydrates the form when it is still clean", () => {
		const current = payload({ ...filled, mode: "relay" });
		const next: UpdateConfigInput = {
			...current,
			relay_ingest_url: "https://relay.replayvod.com/u/new-token",
		};

		expect(formValuesAfterConfigRefresh(current, current, next)).toBe(next);
	});

	it("preserves unsaved edits during a config refetch", () => {
		const previous = payload({ ...filled, mode: "relay" });
		const edited: UpdateConfigInput = {
			...previous,
			relay_ingest_url: "https://relay.replayvod.com/u/unsaved",
		};
		const refetched: UpdateConfigInput = {
			...previous,
			relay_ingest_url: "https://relay.replayvod.com/u/refetched",
		};

		expect(formValuesAfterConfigRefresh(edited, previous, refetched)).toBe(
			edited,
		);
	});

	it("marks the form dirty once values diverge from the saved baseline", () => {
		const baseline = payload({ ...filled, mode: "relay" });
		const edited: UpdateConfigInput = {
			...baseline,
			relay_subscribe_url: "wss://relay.replayvod.com/u/unsaved/subscribe",
		};

		expect(formValuesDirty(baseline, baseline)).toBe(false);
		expect(formValuesDirty(edited, baseline)).toBe(true);
	});
});

describe("EventSubSetupCard rendered form", () => {
	it("submits edited relay fields through the mutation payload", () => {
		updateMock.mutate.mockClear();
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

		expect(updateMock.mutate).toHaveBeenCalledWith(
			{
				mode: "relay",
				webhook_callback_url: "",
				relay_ingest_url: "https://relay.replayvod.com/u/edited",
				relay_subscribe_url: "wss://relay.replayvod.com/u/edited/subscribe",
				relay_local_callback_url:
					"http://127.0.0.1:9090/api/v1/webhook/callback",
			},
			expect.objectContaining({ onSuccess: expect.any(Function) }),
		);
	});

	it("submits the direct webhook field through the mutation payload", () => {
		updateMock.mutate.mockClear();
		render(
			createElement(EventSubSetupCard, {
				data: configResponse({
					mode: "direct",
					uses_relay_agent: false,
					relay_ingest_url: undefined,
					relay_subscribe_url: undefined,
					relay_local_callback_url: undefined,
					webhook_callback_url:
						"https://replayvod.example/api/v1/webhook/callback",
					active: {
						source: "app",
						mode: "direct",
						creates_twitch_subscriptions: true,
						uses_relay_agent: false,
						polls_helix: false,
					},
				}),
			}),
		);

		fireEvent.change(screen.getByLabelText("eventsub.webhook_callback_url"), {
			target: { value: " https://new.example/api/v1/webhook/callback " },
		});
		fireEvent.click(
			screen.getByRole("button", { name: "eventsub.save_config" }),
		);

		expect(updateMock.mutate).toHaveBeenCalledWith(
			{
				mode: "direct",
				webhook_callback_url: "https://new.example/api/v1/webhook/callback",
				relay_ingest_url: "",
				relay_subscribe_url: "",
				relay_local_callback_url: "",
			},
			expect.objectContaining({ onSuccess: expect.any(Function) }),
		);
	});
});
