import React from "react";
import { useTranslation } from "react-i18next";
import TableEvents from "../../components/Table/TableEvents";
import { EventLog } from "../../type";
import { ApiRoutes } from "../../type/routes";
import { customFetch } from "../../utils/utils";
import { useQuery } from "@tanstack/react-query";
import TitledLayout from "../../components/Layout/TitledLayout";

const EventsPage: React.FC = () => {
    const { t } = useTranslation();

    const {
        data: logs,
        isLoading,
        isError,
        error,
    } = useQuery<EventLog[], Error>({
        queryKey: ["logs"],
        queryFn: (): Promise<EventLog[]> => customFetch(ApiRoutes.GET_LOG_DOMAINS),
        staleTime: 5 * 60 * 1000,
    });

    if (isLoading) {
        return <div>{t("Loading")}</div>;
    }

    if (isError || !logs) {
        return <div>Error: {error?.message}</div>;
    }

    return (
        <TitledLayout title={t("Log Events")}>
            {isLoading ? <div>{t("Loading")}</div> : <TableEvents items={logs} />}
        </TitledLayout>
    );
};

export default EventsPage;
