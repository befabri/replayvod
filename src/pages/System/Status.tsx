import React, { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import { EventSubCost } from "../../type";

const Tasks: React.FC = () => {
    const { t } = useTranslation();
    const [status, setStatus] = useState<EventSubCost | null>(null);
    const ROOT_URL = import.meta.env.VITE_ROOTURL;
    const [isLoading, setIsLoading] = useState(true);

    useEffect(() => {
        const fetchData = async () => {
            const response = await fetch(`${ROOT_URL}/api/twitch/eventsub/costs`, {
                credentials: "include",
            });
            if (!response.ok) {
                throw new Error(`HTTP error! status: ${response.status}`);
            }
            const data = await response.json();
            console.log(data);
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

export default Tasks;
