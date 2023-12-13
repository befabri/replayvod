import React, { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import TableTasks from "../../components/Table/TableTasks";
import { Task } from "../../type";
import { ApiRoutes, getApiRoute } from "../../type/routes";

const TasksPage: React.FC = () => {
    const { t } = useTranslation();
    const [tasks, setTasks] = useState<Task[]>([]);
    const [isLoading, setIsLoading] = useState(true);

    useEffect(() => {
        const fetchData = async () => {
            const url = getApiRoute(ApiRoutes.GET_TASK);
            const response = await fetch(url, {
                credentials: "include",
            });
            if (!response.ok) {
                throw new Error(`HTTP error! status: ${response.status}`);
            }
            const data = await response.json();
            setTasks(data);
            setIsLoading(false);
        };

        fetchData();
        const intervalId = setInterval(fetchData, 10000);

        return () => clearInterval(intervalId);
    }, []);

    return (
        <div className="p-4">
            <div className="p-4 mt-14">
                <h1 className="text-3xl font-bold pb-5 dark:text-stone-100">{t("Tasks Scheduled")}</h1>
            </div>
            {isLoading ? <div>{t("Loading")}</div> : <TableTasks items={tasks} />}
        </div>
    );
};

export default TasksPage;
