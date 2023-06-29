import React, { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import TableTasks from "../../components/TableTasks";
import { Task } from "../../type";

const Tasks: React.FC = () => {
    const { t } = useTranslation();
    const [status, setStatus] = useState<Task[]>([]);
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
            setStatus(data);
            setIsLoading(false);
        };

        fetchData();
        const intervalId = setInterval(fetchData, 10000);

        return () => clearInterval(intervalId);
    }, []);
    return (
        <div className="p-4 sm:ml-64">
            <div className="p-4 mt-14">
                <h1 className="text-3xl font-bold pb-5 dark:text-stone-100">{t("Status")}</h1>
            </div>
            {isLoading ? <div>{t("Loading")}</div> : <TableTasks items={status} />}
        </div>
    );
};

export default Tasks;
