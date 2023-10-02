import React, { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import TableSchedule from "../../components/Table/TableSchedule";
import { EventSub } from "../../type";
import { ApiRoutes, getApiRoute } from "../../type/routes";

const Manage: React.FC = () => {
    const { t } = useTranslation();
    const [eventSubs, setEventSubs] = useState<EventSub[]>([]);
    const [isLoading, setIsLoading] = useState(true);

    useEffect(() => {
        const fetchData = async () => {
            let url = getApiRoute(ApiRoutes.GET_TWITCH_EVENTSUB_SUBSCRIPTIONS);
            const response = await fetch(url, {
                credentials: "include",
            });
            if (!response.ok) {
                throw new Error(`HTTP error! status: ${response.status}`);
            }
            const data = await response.json();
            setEventSubs(data.data || []);
            setIsLoading(false);
        };

        fetchData();
        const intervalId = setInterval(fetchData, 10000);

        return () => clearInterval(intervalId);
    }, []);
    return (
        <div className="p-4">
            <div className="p-4 mt-14">
                <h1 className="text-3xl font-bold pb-5 dark:text-stone-100">{t("EventSub subscriptions")}</h1>
            </div>
            {isLoading ? <div>{t("Loading")}</div> : <TableSchedule items={eventSubs} />}
        </div>
    );
};

export default Manage;
