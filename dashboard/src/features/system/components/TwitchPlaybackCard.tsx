import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import { browserCredentialTransportAllowed } from "@/api/credential-transport";
import { useTRPC, useTRPCClient } from "@/api/trpc";
import { handleApiError } from "@/api/unauthorized";
import { Alert } from "@/components/ui/alert";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import {
	Card,
	CardContent,
	CardDescription,
	CardHeader,
	CardTitle,
} from "@/components/ui/card";
import { Checkbox } from "@/components/ui/checkbox";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { LoadingState } from "@/components/ui/loading-state";
import { BadgeSkeleton, ButtonSkeleton } from "@/components/ui/skeleton";
import { API_URL } from "@/env";

const STATUS_ROW_CLASS = "flex min-h-9 flex-wrap items-center gap-3";

export function TwitchPlaybackCard() {
	const { t } = useTranslation();
	const trpc = useTRPC();
	const client = useTRPCClient();
	const queryClient = useQueryClient();
	const queryOptions = trpc.twitchPlayback.status.queryOptions();
	const status = useQuery({ ...queryOptions, refetchInterval: 60_000 });
	const [transportAllowed, setTransportAllowed] = useState(false);
	useEffect(() => {
		setTransportAllowed(browserCredentialTransportAllowed(API_URL));
	}, []);
	const [token, setToken] = useState("");
	const [consent, setConsent] = useState(false);
	const [busy, setBusy] = useState(false);
	const [error, setError] = useState("");
	const [message, setMessage] = useState("");

	async function act(action: "connect" | "check" | "disconnect") {
		if (action === "connect" && !browserCredentialTransportAllowed(API_URL)) {
			setToken("");
			setConsent(false);
			setError(t("twitch_playback.https_required"));
			return;
		}
		if (busy || (action === "connect" && (!consent || !token.trim()))) return;
		setBusy(true);
		setError("");
		setMessage("");
		const submittedToken = action === "connect" ? token : "";
		setToken("");
		setConsent(false);
		try {
			const saved =
				action === "connect"
					? await client.twitchPlayback.connect.mutate({
							session_token: submittedToken,
							consent: true,
						})
					: action === "check"
						? await client.twitchPlayback.check.mutate()
						: await client.twitchPlayback.disconnect.mutate();
			await queryClient.cancelQueries({ queryKey: queryOptions.queryKey });
			queryClient.setQueryData(queryOptions.queryKey, saved);
			await queryClient.resetQueries({
				queryKey: trpc.video.liveRenditions.pathKey(),
			});
			setMessage(t(`twitch_playback.${action}_success`));
		} catch (err) {
			handleApiError(err);
			setError(
				err instanceof Error ? err.message : t("twitch_playback.failed"),
			);
		} finally {
			setBusy(false);
		}
	}

	const connected = status.data && status.data.state !== "disconnected";
	return (
		<Card>
			<CardHeader>
				<CardTitle>{t("twitch_playback.card_title")}</CardTitle>
				<CardDescription>{t("twitch_playback.description")}</CardDescription>
			</CardHeader>
			<CardContent className="space-y-5">
				{status.isPending && (
					<LoadingState className={STATUS_ROW_CLASS}>
						<BadgeSkeleton />
					</LoadingState>
				)}
				{status.isError && (
					<Alert variant="destructive">
						{t("twitch_playback.load_failed")}
					</Alert>
				)}
				{status.data && (
					<div className={STATUS_ROW_CLASS}>
						<Badge
							variant={status.data.state === "connected" ? "green" : "muted"}
						>
							{t(`twitch_playback.${status.data.state}`)}
						</Badge>
						{connected && <span>{status.data.login}</span>}
						{connected && (
							<>
								<Button
									variant="outline"
									disabled={busy}
									onClick={() => void act("check")}
								>
									{t("twitch_playback.check")}
								</Button>
								<Button
									variant="outline"
									disabled={busy}
									onClick={() => void act("disconnect")}
								>
									{t("twitch_playback.disconnect")}
								</Button>
							</>
						)}
					</div>
				)}
				{status.data?.state === "reconnect_required" && (
					<Alert variant="destructive">
						{t("twitch_playback.reconnect_hint")}
					</Alert>
				)}
				<p className="text-sm text-muted-foreground">
					{t("twitch_playback.shared_hint")}
				</p>
				<details className="rounded-md border p-3 text-sm">
					<summary className="cursor-pointer font-medium">
						{t("twitch_playback.how_to")}
					</summary>
					<ol className="list-decimal space-y-2 pl-5 pt-3">
						<li>
							<a
								href="https://www.twitch.tv"
								target="_blank"
								rel="noreferrer"
								className="underline"
							>
								{t("twitch_playback.step_login")}
							</a>
						</li>
						<li>{t("twitch_playback.step_devtools")}</li>
						<li>{t("twitch_playback.step_cookie")}</li>
					</ol>
				</details>
				{!transportAllowed && (
					<Alert variant="destructive">
						{t("twitch_playback.https_required")}
					</Alert>
				)}
				<form
					className="space-y-4"
					onSubmit={(event) => {
						event.preventDefault();
						void act("connect");
					}}
				>
					<div className="space-y-2">
						<Label htmlFor="twitch-playback-token">
							{t("twitch_playback.token_label")}
						</Label>
						<Input
							id="twitch-playback-token"
							name="twitch-playback-token"
							type="password"
							autoComplete="off"
							spellCheck={false}
							autoCapitalize="none"
							maxLength={1024}
							value={token}
							disabled={busy || !transportAllowed}
							onChange={(event) => setToken(event.target.value)}
							aria-describedby="twitch-playback-token-hint"
						/>
						<p
							id="twitch-playback-token-hint"
							className="text-xs text-muted-foreground"
						>
							{t("twitch_playback.token_hint")}
						</p>
					</div>
					<div className="flex items-start gap-2">
						<Checkbox
							id="twitch-playback-consent"
							checked={consent}
							disabled={busy || !transportAllowed}
							onCheckedChange={(checked) => setConsent(checked === true)}
						/>
						<Label
							htmlFor="twitch-playback-consent"
							className="text-sm font-normal leading-relaxed"
						>
							{t("twitch_playback.consent")}
						</Label>
					</div>
					{status.isPending ? (
						<ButtonSkeleton />
					) : (
						<Button
							type="submit"
							disabled={
								busy ||
								!transportAllowed ||
								!consent ||
								!token.trim() ||
								!status.data
							}
						>
							{busy
								? t("common.loading")
								: t(
										connected
											? "twitch_playback.replace"
											: "twitch_playback.connect",
									)}
						</Button>
					)}
				</form>
				{error && <Alert variant="destructive">{error}</Alert>}
				{message && <Alert variant="success">{message}</Alert>}
				<p className="text-sm text-muted-foreground">
					{t("twitch_playback.quality_hint")}
				</p>
			</CardContent>
		</Card>
	);
}
