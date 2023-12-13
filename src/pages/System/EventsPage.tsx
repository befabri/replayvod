import React, { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import TableEvents from "../../components/Table/TableEvents";
import { EventLog } from "../../type";
import { ApiRoutes, getApiRoute } from "../../type/routes";

const EventsPage: React.FC = () => {
    const { t } = useTranslation();
    const [logs, setLogs] = useState<EventLog[]>([]);
    const [isLoading, setIsLoading] = useState(true);

    useEffect(() => {
        const fetchData = async () => {
            const url = getApiRoute(ApiRoutes.GET_LOG_DOMAINS);
            const response = await fetch(url, {
                credentials: "include",
            });
            if (!response.ok) {
                throw new Error(`HTTP error! status: ${response.status}`);
            }
            const data = await response.json();
            setLogs(data);
            setIsLoading(false);
        };

        fetchData();
        const intervalId = setInterval(fetchData, 10000);

        return () => clearInterval(intervalId);
    }, []);
    return (
        <div className="p-4">
            <div className="p-4 mt-14">
                <h1 className="text-3xl font-bold pb-5 dark:text-stone-100">{t("Log Files")}</h1>
            </div>
            {isLoading ? <div>{t("Loading")}</div> : <TableEvents items={logs} />}
        </div>
    );
};

export default EventsPage;
