import { Link } from "@tanstack/react-router";
import type { ComponentProps } from "react";
import { useTranslation } from "react-i18next";
import { Button, buttonVariants } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { cn } from "@/lib/utils";
import { useRelatedRecordings } from "../queries";
import { VideoStatusBadge } from "./VideoStatusBadge";

function RelatedLink({
	videoId,
	...props
}: { videoId: number } & Pick<
	ComponentProps<"a">,
	"children" | "className" | "aria-current"
>) {
	return (
		<Link
			to="/dashboard/watch/$videoId"
			params={{ videoId: String(videoId) }}
			search={{ t: undefined }}
			{...props}
		/>
	);
}

export function RelatedRecordings({ videoId }: { videoId: number }) {
	const { t } = useTranslation();
	const { data, error, refetch, isFetching } = useRelatedRecordings(videoId);
	const retry = error ? (
		<Button
			variant="link"
			size="sm"
			disabled={isFetching}
			onClick={() => void refetch()}
		>
			{t("watch.related_retry")}
		</Button>
	) : null;
	if (!data) return retry;
	if (!data.intent_id || data.items.length < 2) return retry;
	const current = data.items.findIndex((item) => item.id === videoId);
	if (current < 0) return retry;
	const previous = data.items[current - 1];
	const next = data.items[current + 1];
	return (
		<nav aria-label={t("watch.related_title")} data-testid="related-recordings">
			<Card className="p-0 gap-0">
				<CardHeader className="flex-row flex-wrap items-center justify-between gap-2">
					<CardTitle className="text-sm">{t("watch.related_title")}</CardTitle>
					{data.status === "waiting" && (
						<span className="text-sm text-muted-foreground">
							{t("watch.related_waiting")}
						</span>
					)}
					{data.status === "active" && (
						<span className="text-sm text-muted-foreground">
							{t("watch.related_active")}
						</span>
					)}
					{retry}
				</CardHeader>
				<CardContent className="p-4 space-y-3">
					<div className="flex gap-4 text-sm">
						{previous && (
							<RelatedLink
								videoId={previous.id}
								className={buttonVariants({ variant: "link", size: "inline" })}
							>
								{t("watch.related_previous")}
							</RelatedLink>
						)}
						{next && (
							<RelatedLink
								videoId={next.id}
								className={cn(
									buttonVariants({ variant: "link", size: "inline" }),
									"ml-auto",
								)}
							>
								{t("watch.related_next")}
							</RelatedLink>
						)}
					</div>
					<ol className="space-y-2 text-sm">
						{data.items.map((item) => (
							<li key={item.id}>
								<RelatedLink
									videoId={item.id}
									aria-current={item.id === videoId ? "page" : undefined}
									className="flex flex-wrap justify-between gap-2 rounded p-2 hover:bg-muted aria-[current=page]:bg-muted"
								>
									<span>
										{item.position}. {item.title || t("watch.related_untitled")}
									</span>
									<span className="flex flex-wrap items-center gap-2 text-muted-foreground">
										<time dateTime={item.started_at}>
											{new Date(item.started_at).toLocaleString()}
										</time>
										·{" "}
										{item.deleted_at ? (
											t("watch.related_removed")
										) : (
											<VideoStatusBadge
												status={item.status}
												completionKind={item.completion_kind}
												t={t}
											/>
										)}
									</span>
								</RelatedLink>
							</li>
						))}
					</ol>
				</CardContent>
			</Card>
		</nav>
	);
}
