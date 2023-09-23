import React, { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import TableSchedule from "../../components/TableSchedule";
import { EventSub } from "../../type";

const Manage: React.FC = () => {
    const { t } = useTranslation();
    const [eventSubs, setEventSubs] = useState<EventSub[]>([]);
    const ROOT_URL = import.meta.env.VITE_ROOTURL;
    const [isLoading, setIsLoading] = useState(true);

    useEffect(() => {
        const fetchData = async () => {
            const response = await fetch(`${ROOT_URL}/api/twitch/eventsub/subscriptions`, {
                credentials: "include",
            });
            if (!response.ok) {
                throw new Error(`HTTP error! status: ${response.status}`);
            }
            const data = await response.json();
            console.log(data);
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
