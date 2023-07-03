import { Key } from "react";
import { useTranslation } from "react-i18next";
import { Log } from "../type";
import { capitalizeFirstLetter } from "../utils/utils";

const TableLogs: React.FC<{ items: Log[] }> = ({ items }) => {
    const { t } = useTranslation();
    const ROOT_URL = import.meta.env.VITE_ROOTURL;

    const fields: (keyof Log)[] = ["filename", "type", "lastWriteTime"];

    const formatTime = (dateString: string) => {
        let date = new Date(dateString);
        return `${date.getHours().toString().padStart(2, "0")}:${date
            .getMinutes()
            .toString()
            .padStart(2, "0")}:${date.getSeconds().toString().padStart(2, "0")}`;
    };

    const fetchAndShowLog = async (id: string) => {
        const response = await fetch(`${ROOT_URL}/api/log/files/${id}`, {
            credentials: "include",
        });

        if (!response.ok) {
            throw new Error(`HTTP error! status: ${response.status}`);
        }

        const text = await response.text();

        const blob = new Blob([text], { type: "text/plain;charset=utf-8;" });
        const url = URL.createObjectURL(blob);

        window.open(url);
    };

    return (
        <div className="relative overflow-x-auto shadow-md sm:rounded-lg">
            <table className="w-full text-sm text-left text-gray-500 dark:text-gray-400">
                <thead className="text-xs text-gray-700 uppercase bg-gray-50 dark:bg-gray-700 dark:text-gray-400">
                    <tr>
                        {fields.map((field, index) => (
                            <th key={index} scope="col" className="px-6 py-3">
                                <div className="flex items-center"> {t(capitalizeFirstLetter(field))}</div>
                            </th>
                        ))}
                        <th scope="col" className="px-6 py-3"></th>
                    </tr>
                </thead>
                <tbody className="divide-y divide-gray-200 dark:divide-gray-700">
                    {items.map((item: Log, index: Key | null | undefined) => (
                        <tr key={index}>
                            <td className="px-6 py-4 whitespace-nowrap">{item.filename}</td>
                            <td className="px-6 py-4 whitespace-nowrap">{t(item.type)}</td>
                            <td className="px-6 py-4 whitespace-nowrap">{formatTime(item.lastWriteTime)}</td>
                            <td className="px-6 py-4 whitespace-nowrap">
                                <button onClick={() => fetchAndShowLog(String(item.id))} className="text-blue-500">
                                    {t("View Logfile")}
                                </button>
                            </td>
                        </tr>
                    ))}
                </tbody>
            </table>
        </div>
    );
};

export default TableLogs;
