import React from "react";
import { useTranslation } from "react-i18next";
import TableSchedule from "../../components/Table/TableSchedule";
import { EventSub } from "../../type";
import { ApiRoutes } from "../../type/routes";
import { useQuery } from "@tanstack/react-query";
import { customFetch } from "../../utils/utils";
import NotFound from "../../components/Others/NotFound";
import Container from "../../components/Layout/Container";
import Title from "../../components/Typography/TitleComponent";

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
        staleTime: 60 * 60 * 1000,
    });

    if (isLoading) {
        return <div>{t("Loading")}</div>;
    }

    if (isError || !eventSubs) {
        return <div>Error: {error?.message}</div>;
    }

    return (
        <Container>
            <Title title={t("EventSub subscriptions")} />
            {eventSubs.data ? (
                <div className="flex  flex-col gap-3">
                    <span className="pb-5 dark:text-stone-100">
                        {t("The number of total EventSub subscription is ")}
                        {eventSubs.data.cost.total}
                        <br />
                        {eventSubs.data.cost.total_cost}/{eventSubs.data.cost.max_total_cost}
                    </span>
                    <TableSchedule items={eventSubs.data.list} />
                </div>
            ) : (
                <NotFound text={t("There is no EventSub subscription")} />
            )}
        </Container>
    );
};

export default EventSubPage;
