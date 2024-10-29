import { Key } from "react";
import { useTranslation } from "react-i18next";
import { Log } from "../../type";
import { capitalizeFirstLetter } from "../../utils/utils";
import { getApiRoute, ApiRoutes } from "../../type/routes";

const TableLogs: React.FC<{ items: Log[] }> = ({ items }) => {
    const { t } = useTranslation();
    const fields: (keyof Log)[] = ["filename", "lastWriteTime"];

    const fetchAndShowLog = async (id: string) => {
        const url = getApiRoute(ApiRoutes.GET_LOG_FILES_ID, "id", id);
        const response = await fetch(url, {
            credentials: "include",
        });
        if (!response.ok) {
            throw new Error(`HTTP error! status: ${response.status}`);
        }

        const text = await response.text();

        const blob = new Blob([text], { type: "text/plain;charset=utf-8;" });
        const urlBlob = URL.createObjectURL(blob);

        window.open(urlBlob);
    };

    return (
        <div className="relative overflow-x-auto shadow-md sm:rounded-lg">
            <table className="w-full text-sm text-left text-gray-500 dark:text-gray-400">
                <thead className="text-xs text-gray-700 uppercase bg-gray-50 dark:bg-custom_lightblue dark:text-gray-400">
                    <tr>
                        {fields.map((field, index) => (
                            <th key={index} scope="col" className="px-6 py-3">
                                <div className="flex items-center"> {t(capitalizeFirstLetter(field))}</div>
                            </th>
                        ))}
                        <th scope="col" className="px-6 py-3"></th>
                    </tr>
                </thead>
                <tbody className="divide-y-2 divide-gray-200 dark:divide-custom_lightblue">
                    {items.map((item: Log, index: Key | null | undefined) => (
                        <tr key={index}>
                            <td className="px-6 py-4 whitespace-nowrap">{item.filename}</td>
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
