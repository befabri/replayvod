import React from "react";
import { useTranslation } from "react-i18next";
import TableSchedule from "../../components/Table/TableSchedule";
import { EventSub } from "../../type";
import { ApiRoutes } from "../../type/routes";
import { useQuery } from "@tanstack/react-query";
import { customFetch } from "../../utils/utils";

const EventSubPage: React.FC = () => {
    const { t } = useTranslation();

    const {
        data: eventSubs,
        isLoading,
        isError,
        error,
    } = useQuery<EventSub, Error>({
        queryKey: ["event-sub"],
        queryFn: (): Promise<EventSub> => customFetch(ApiRoutes.GET_EVENT_SUB_SUBSCRIPTIONS),
        staleTime: 5 * 60 * 1000,
    });

    if (isLoading) {
        return <div>{t("Loading")}</div>;
    }

    if (isError || !eventSubs) {
        return <div>Error: {error?.message}</div>;
    }

    return (
        <div className="p-4">
            <div className="mt-14 p-4">
                <h1 className="pb-5 text-3xl font-bold dark:text-stone-100">{t("EventSub subscriptions")}</h1>
            </div>
            {isLoading ? <div>{t("Loading")}</div> : <TableSchedule items={eventSubs.data} />}
        </div>
    );
};

export default EventSubPage;
