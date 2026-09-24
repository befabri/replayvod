import type {
	RecordingWebhookConfigResponse,
	RecordingWebhookDeliveryResponse,
} from "@/api/generated/trpc";
import { FIXTURE_NOW } from "./videos";

export function makeRecordingWebhookConfig(
	overrides: Partial<RecordingWebhookConfigResponse> = {},
): RecordingWebhookConfigResponse {
	return {
		enabled: true,
		url: "https://hooks.example.com/replayvod",
		secret: "whsec_4f9a2c7e1b8d3f6a0c5e9b2d7f1a4c8e",
		events: [],
		...overrides,
	};
}

export function makeWebhookDelivery(
	index = 0,
	overrides: Partial<RecordingWebhookDeliveryResponse> = {},
): RecordingWebhookDeliveryResponse {
	return {
		id: index + 1,
		time: new Date(FIXTURE_NOW - index * 45 * 60_000).toISOString(),
		event: index % 2 === 0 ? "recording.completed" : "recording.failed",
		video_id: 100 + index,
		outcome: "delivered",
		status: 200,
		attempts: 1,
		message_id: `msg-${(index + 1).toString().padStart(4, "0")}`,
		...overrides,
	};
}

export function makeWebhookDeliveries(
	count: number,
): RecordingWebhookDeliveryResponse[] {
	return Array.from({ length: count }, (_, index) =>
		makeWebhookDelivery(index),
	);
}
