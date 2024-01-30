import React from "react";
import { useTranslation } from "react-i18next";
import { EventSubCost } from "../../type";
import { ApiRoutes } from "../../type/routes";
import NotFound from "../../components/Others/NotFound";
import { customFetch } from "../../utils/utils";
import { useQuery } from "@tanstack/react-query";

const StatusPage: React.FC = () => {
    const { t } = useTranslation();

    const {
        data: status,
        isLoading,
        isError,
        error,
    } = useQuery<EventSubCost, Error>({
        queryKey: ["status"],
        queryFn: (): Promise<EventSubCost> => customFetch(ApiRoutes.GET_EVENT_SUB_COSTS),
        staleTime: 5 * 60 * 1000,
    });

    if (isLoading) {
        return <div>{t("Loading")}</div>;
    }

    if (isError || !status) {
        return <div>Error: {error?.message}</div>;
    }

    return (
        <div className="p-4">
            <div className="mt-14 p-4">
                <h1 className="pb-5 text-3xl font-bold dark:text-stone-100">{t("Status")}</h1>
            </div>
            {status.data ? (
                <span className="pb-5 dark:text-stone-100">
                    {t("The number of total EventSub subscription is ")}
                    {status.data.total}
                    <br />
                    {status.data.total_cost}/{status.data.max_total_cost}
                </span>
            ) : (
                <NotFound text={t("There is no EventSub subscription")} />
            )}
        </div>
    );
};

export default StatusPage;
