import { useState } from "react";
import Icon from "./IconSort";
import { TableProps, EventSub } from "../type";
import { useTranslation } from "react-i18next";
import { capitalizeFirstLetter, formatDate, truncateString } from "../utils/utils";

const TableSchedule = ({ items: initialItems }: any) => {
    console.log("===> ", initialItems);
    const { t } = useTranslation();
    const [sortField, setSortField] = useState<keyof EventSub | null>(null);
    const [sortDirection, setSortDirection] = useState<"asc" | "desc">("asc");
    const [items, setItems] = useState<EventSub[]>(initialItems);

    const handleSort = (field: keyof EventSub) => {
        let direction: "asc" | "desc" = "asc";
        if (field === sortField) {
            direction = sortDirection === "asc" ? "desc" : "asc";
        }
        setSortField(field);
        setSortDirection(direction);
        sortData(items, field, direction);
    };

    const sortData = (data: EventSub[], field: keyof EventSub, direction: "asc" | "desc") => {
        const sortedData = [...data].sort((a, b) => {
            let aField = a[field];
            let bField = b[field];

            if (aField === undefined || bField === undefined) return 0;

            if (typeof aField === "string" && typeof bField === "string") {
                const lowerAField = aField.toLowerCase();
                const lowerBField = bField.toLowerCase();

                if (lowerAField < lowerBField) return direction === "asc" ? -1 : 1;
                if (lowerAField > lowerBField) return direction === "asc" ? 1 : -1;
            } else {
                if (aField < bField) return direction === "asc" ? -1 : 1;
                if (aField > bField) return direction === "asc" ? 1 : -1;
            }

            return 0;
        });

        setItems(sortedData);
    };

    const fields: (keyof EventSub)[] = [
        "id",
        "status",
        "type",
        "broadcaster_user_id",
        "broadcaster_login",
        "created_at",
        "cost",
    ];

    return (
        <div className="relative overflow-x-auto shadow-md sm:rounded-lg">
            <table className="w-full text-sm text-left text-gray-500 dark:text-gray-400">
                <thead className="text-xs text-gray-700 uppercase bg-gray-50 dark:bg-gray-700 dark:text-gray-400">
                    <tr>
                        {fields.map((field, index) => (
                            <th key={index} scope="col" className="px-6 py-3">
                                <div className="flex items-center">
                                    {t(capitalizeFirstLetter(field).replaceAll("_", " "))}
                                    <Icon onClick={() => handleSort(field)} />
                                </div>
                            </th>
                        ))}
                    </tr>
                </thead>
                <tbody>
                    {items.map((eventSub, idx) => (
                        <tr
                            key={idx}
                            className="bg-white border-b dark:bg-gray-800 dark:border-gray-700 hover:bg-gray-50 dark:hover:bg-gray-600">
                            <td className="px-6 py-4" title={eventSub.broadcaster_user_id}>
                                {eventSub.broadcaster_user_id}
                            </td>
                            <td className="px-6 py-4" title={eventSub.broadcaster_login}>
                                {eventSub.broadcaster_login}
                            </td>
                            <td className="px-6 py-4" title={eventSub.type}>
                                {eventSub.type}
                            </td>{" "}
                            <td className="px-6 py-4" title={eventSub.status}>
                                {eventSub.status}
                            </td>
                            <td className="px-6 py-4" title={formatDate(eventSub.created_at, "Europe/Paris")}>
                                {formatDate(eventSub.created_at, "Europe/Paris")}
                            </td>{" "}
                            <td className="px-6 py-4" title={eventSub.cost.toString()}>
                                {eventSub.cost}
                            </td>
                        </tr>
                    ))}
                </tbody>
            </table>
        </div>
    );
};

export default TableSchedule;
