import { useState } from "react";
import Icon from "../UI/Icon/IconSort";
import { EventSub } from "../../type";
import { useTranslation } from "react-i18next";
import { capitalizeFirstLetter, formatDate } from "../../utils/utils";

const TableSchedule = ({ items: initialItems }: { items: EventSub["data"]["list"] }) => {
    const { t } = useTranslation();
    const [sortField, setSortField] = useState<keyof EventSub["data"]["list"][number] | null>(null);
    const [sortDirection, setSortDirection] = useState<"asc" | "desc">("asc");
    const [items, setItems] = useState<EventSub["data"]["list"]>(initialItems);

    const storedTimeZone = localStorage.getItem("timeZone") || "Europe/London";

    const handleSort = (field: keyof EventSub["data"]["list"][number]) => {
        let direction: "asc" | "desc" = "asc";
        if (field === sortField) {
            direction = sortDirection === "asc" ? "desc" : "asc";
        }
        setSortField(field);
        setSortDirection(direction);
        sortData(items, field, direction);
    };

    const sortData = (
        data: EventSub["data"]["list"],
        field: keyof EventSub["data"]["list"][number],
        direction: "asc" | "desc"
    ) => {
        const sortedData = [...data].sort((a, b) => {
            const aField = a[field];
            const bField = b[field];

            if (aField === undefined || bField === undefined) return 0;

            if (typeof aField === "string" && typeof bField === "string") {
                return direction === "asc" ? aField.localeCompare(bField) : bField.localeCompare(aField);
            } else if (typeof aField === "number" && typeof bField === "number") {
                return direction === "asc" ? aField - bField : bField - aField;
            }

            return 0;
        });

        setItems(sortedData);
    };

    const fields: (keyof EventSub["data"]["list"][number])[] = [
        "id",
        "subscriptionType",
        "status",
        "broadcasterId",
        "createdAt",
        "cost",
    ];

    return (
        <div className="relative overflow-x-auto shadow-md sm:rounded-lg">
            <table className="w-full text-left text-sm text-gray-500 dark:text-gray-400">
                <thead className="bg-gray-50 text-xs uppercase text-gray-700 dark:bg-custom_lightblue dark:text-gray-400">
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
                            className="border-b bg-white hover:bg-gray-50 dark:border-custom_lightblue dark:bg-custom_blue dark:hover:bg-custom_lightblue">
                            <td className="px-6 py-4" title={eventSub.broadcasterId}>
                                {eventSub.id}
                            </td>
                            <td className="px-6 py-4" title={eventSub.subscriptionType}>
                                {eventSub.subscriptionType}
                            </td>
                            <td className="px-6 py-4" title={eventSub.status}>
                                {eventSub.status}
                            </td>
                            <td className="px-6 py-4" title={formatDate(eventSub.createdAt, storedTimeZone)}>
                                {formatDate(eventSub.createdAt, storedTimeZone)}
                            </td>
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
