// components/TableTasks.tsx
import { useState } from "react";
import { Task } from "../../type";
import { Icon } from "@iconify/react";
import { useTranslation } from "react-i18next";
import { getApiRoute, ApiRoutes } from "../../type/routes";
import { formatInterval, formatIntervalFuture, formatIntervalPast } from "../../utils/utils";

const TableTasks: React.FC<{ items: Task[] }> = ({ items: initialItems }) => {
    const { t } = useTranslation();
    const [items, setItems] = useState<Task[]>(initialItems);
    const [loadingTasks, setLoadingTasks] = useState<string[]>([]);

    const fetchTaskData = async (id: string) => {
        setLoadingTasks((prev) => [...prev, id]);
        const url = getApiRoute(ApiRoutes.GET_TASK_RUN_ID, "id", id);
        const response = await fetch(url, {
            credentials: "include",
        });
        const data = await response.json();
        const updatedItems = items.map((item) => (item.id === id ? data : item));
        setItems(updatedItems);
        setLoadingTasks((prev) => prev.filter((taskId) => taskId !== id));
    };

    const fields: (keyof Task)[] = ["name", "interval", "lastExecution", "lastDuration", "nextExecution"];

    return (
        <div className="relative overflow-x-auto shadow-md sm:rounded-lg">
            <table className="w-full text-sm text-left text-gray-500 dark:text-gray-400">
                <thead className="text-xs text-gray-700 uppercase bg-gray-50 dark:bg-gray-700 dark:text-gray-400">
                    <tr>
                        {fields.map((field, index) => (
                            <th key={index} scope="col" className="px-6 py-3">
                                <div className="flex items-center">{field}</div>
                            </th>
                        ))}
                        <th scope="col" className="px-6 py-3">
                            Refresh
                        </th>
                    </tr>
                </thead>
                <tbody className="divide-y divide-gray-200 dark:divide-gray-700">
                    {items.map((item, index) => (
                        <tr key={index}>
                            <td className="px-6 py-4 whitespace-nowrap" title={item.name}>
                                {t(item.name)}
                            </td>
                            <td
                                className="px-6 py-4 whitespace-nowrap"
                                title={formatInterval(item.interval).toString()}>
                                {formatInterval(item.interval)}
                            </td>
                            <td
                                className="px-6 py-4 whitespace-nowrap"
                                title={formatIntervalPast(Date.now() - new Date(item.lastExecution).getTime())}>
                                {formatIntervalPast(Date.now() - new Date(item.lastExecution).getTime())}
                            </td>
                            <td
                                className="px-6 py-4 whitespace-nowrap"
                                title={formatInterval(item.lastDuration).toString()}>
                                {formatInterval(item.lastDuration)}
                            </td>
                            <td
                                className="px-6 py-4 whitespace-nowrap"
                                title={formatIntervalFuture(new Date(item.nextExecution).getTime() - Date.now())}>
                                {formatIntervalFuture(new Date(item.nextExecution).getTime() - Date.now())}
                            </td>
                            <td className="px-6 py-4 whitespace-nowrap">
                                <button
                                    onClick={() => fetchTaskData(item.id)}
                                    className={`p-1 rounded hover:bg-gray-200 dark:hover:bg-gray-700 ${
                                        loadingTasks.includes(item.id) && "animate-spin"
                                    }`}>
                                    <Icon icon="mdi:refresh" width="18" height="18" />
                                </button>
                            </td>
                        </tr>
                    ))}
                </tbody>
            </table>
        </div>
    );
};

export default TableTasks;
