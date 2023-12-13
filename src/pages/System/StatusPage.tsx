import React, { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import { EventSubCost } from "../../type";
import { ApiRoutes, getApiRoute } from "../../type/routes";

const StatusPage: React.FC = () => {
    const { t } = useTranslation();
    const [status, setStatus] = useState<EventSubCost | null>(null);
    const [isLoading, setIsLoading] = useState(true);

    useEffect(() => {
        const fetchData = async () => {
            let url = getApiRoute(ApiRoutes.GET_TWITCH_EVENTSUB_COSTS);
            const response = await fetch(url, {
                credentials: "include",
            });
            if (!response.ok) {
                throw new Error(`HTTP error! status: ${response.status}`);
            }
            const data = await response.json();
            setStatus(data.data);
            setIsLoading(false);
        };

        fetchData();
        const intervalId = setInterval(fetchData, 50000);

        return () => clearInterval(intervalId);
    }, []);
    
    return (
        <div className="p-4">
            <div className="p-4 mt-14">
                <h1 className="text-3xl font-bold pb-5 dark:text-stone-100">{t("Status")}</h1>
            </div>
            {isLoading ? (
                <div>{t("Loading")}</div>
            ) : status ? (
                <span className="pb-5 dark:text-stone-100">
                    {t("The number of total EventSub subscription is ")}
                    {status.total}
                    <br />
                    {status.total_cost}/{status.max_total_cost}
                </span>
            ) : (
                <div className="pb-5 dark:text-stone-100">{t("There is no EventSub subscription")}</div>
            )}
        </div>
    );
};

export default StatusPage;
