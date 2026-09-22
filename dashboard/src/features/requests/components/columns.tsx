import type { ColumnDef } from "@tanstack/react-table";
import type { TFunction } from "i18next";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import type {
	ScheduleRequestResponse,
	ScheduleRequestStatus,
} from "@/features/requests";
import {
	useCancelScheduleRequest,
	useRejectScheduleRequest,
} from "@/features/requests";

type StatusLabelKey =
	| "requests.status_pending"
	| "requests.status_approved"
	| "requests.status_rejected";
const STATUS_LABEL_KEYS: Record<ScheduleRequestStatus, StatusLabelKey> = {
	PENDING: "requests.status_pending",
	APPROVED: "requests.status_approved",
	REJECTED: "requests.status_rejected",
};
const STATUS_VARIANTS: Record<
	ScheduleRequestStatus,
	"default" | "emerald" | "muted"
> = {
	PENDING: "default",
	APPROVED: "emerald",
	REJECTED: "muted",
};

function channelColumn(t: TFunction): ColumnDef<ScheduleRequestResponse> {
	return {
		accessorKey: "broadcaster_name",
		header: t("requests.col_channel"),
		enableSorting: true,
		cell: ({ row }) => {
			const r = row.original;
			return (
				<div className="flex items-center gap-2">
					{r.profile_image_url && (
						<img
							src={r.profile_image_url}
							alt=""
							className="w-8 h-8 rounded-full"
						/>
					)}
					<span>{r.broadcaster_name}</span>
					<span className="text-xs text-muted-foreground">
						@{r.broadcaster_login}
					</span>
				</div>
			);
		},
	};
}

function noteColumn(t: TFunction): ColumnDef<ScheduleRequestResponse> {
	return {
		accessorKey: "note",
		header: t("requests.col_note"),
		enableSorting: false,
		cell: ({ row }) =>
			row.original.note ? (
				<span>{row.original.note}</span>
			) : (
				<span className="text-muted-foreground">—</span>
			),
	};
}

function statusColumn(t: TFunction): ColumnDef<ScheduleRequestResponse> {
	return {
		accessorKey: "status",
		header: t("requests.col_status"),
		enableSorting: true,
		cell: ({ row }) => (
			<Badge variant={STATUS_VARIANTS[row.original.status]}>
				{t(STATUS_LABEL_KEYS[row.original.status])}
			</Badge>
		),
	};
}

function requestedColumn(t: TFunction): ColumnDef<ScheduleRequestResponse> {
	return {
		accessorKey: "created_at",
		header: t("requests.col_requested"),
		enableSorting: true,
		cell: ({ row }) => (
			<span className="text-muted-foreground">
				{new Date(row.original.created_at).toLocaleString()}
			</span>
		),
	};
}

function CancelButton({ id, t }: { id: number; t: TFunction }) {
	const cancel = useCancelScheduleRequest();
	return (
		<button
			type="button"
			disabled={cancel.isPending}
			onClick={() => cancel.mutate({ id })}
			className="text-destructive hover:underline disabled:opacity-60"
		>
			{t("requests.cancel")}
		</button>
	);
}

export function myRequestColumns(
	t: TFunction,
): ColumnDef<ScheduleRequestResponse>[] {
	return [
		channelColumn(t),
		noteColumn(t),
		statusColumn(t),
		requestedColumn(t),
		{
			id: "actions",
			header: () => (
				<span className="text-right w-full block">{t("common.actions")}</span>
			),
			enableSorting: false,
			cell: ({ row }) => (
				<div className="text-right">
					{row.original.status === "PENDING" && (
						<CancelButton id={row.original.id} t={t} />
					)}
				</div>
			),
		},
	];
}

function RejectButton({ id, t }: { id: number; t: TFunction }) {
	const reject = useRejectScheduleRequest();
	return (
		<button
			type="button"
			disabled={reject.isPending}
			onClick={() => reject.mutate({ id })}
			className="text-destructive hover:underline disabled:opacity-60"
		>
			{t("requests.reject")}
		</button>
	);
}

export function adminRequestColumns(
	t: TFunction,
	onApprove: (request: ScheduleRequestResponse) => void,
): ColumnDef<ScheduleRequestResponse>[] {
	return [
		channelColumn(t),
		{
			accessorKey: "requested_by_name",
			header: t("requests.col_requested_by"),
			enableSorting: true,
			cell: ({ row }) => (
				<span className="text-muted-foreground">
					{row.original.requested_by_name}
				</span>
			),
		},
		noteColumn(t),
		statusColumn(t),
		requestedColumn(t),
		{
			id: "actions",
			header: () => (
				<span className="text-right w-full block">{t("common.actions")}</span>
			),
			enableSorting: false,
			cell: ({ row }) =>
				row.original.status === "PENDING" ? (
					<div className="flex items-center justify-end gap-3">
						<Button size="sm" onClick={() => onApprove(row.original)}>
							{t("requests.approve")}
						</Button>
						<RejectButton id={row.original.id} t={t} />
					</div>
				) : null,
		},
	];
}
