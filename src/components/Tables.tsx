import { useState } from "react";
import Icon from "./IconSort";
import Checkbox from "./checkboxProps";
import { Video, TableProps } from "../type";
import { useTranslation } from "react-i18next";
import { capitalizeFirstLetter, formatDate, truncateString } from "../utils/utils";

type ExtendedTableProps = {
    showEdit?: boolean;
    showCheckbox?: boolean;
    showId?: boolean;
    showStatus?: boolean;
} & TableProps;

const Table = ({
    items: initialItems,
    showEdit = true,
    showCheckbox = true,
    showId = true,
    showStatus = true,
}: ExtendedTableProps) => {
    const { t } = useTranslation();
    const [sortField, setSortField] = useState<keyof Video | null>(null);
    const [sortDirection, setSortDirection] = useState<"asc" | "desc">("asc");
    const [items, setItems] = useState<Video[]>(initialItems);
    const [selectAll, setSelectAll] = useState<boolean>(false);

    const handleCheckboxChange = (idx: number, isChecked: boolean) => {
        const newItems = [...items];
        newItems[idx].isChecked = isChecked;
        setItems(newItems);
    };

    const handleSelectAllChange = (isChecked: boolean) => {
        const newItems = items.map((item) => ({ ...item, isChecked }));
        setItems(newItems);
        setSelectAll(isChecked);
    };

    const handleSort = (field: keyof Video) => {
        let direction: "asc" | "desc" = "asc";
        if (field === sortField) {
            direction = sortDirection === "asc" ? "desc" : "asc";
        }
        setSortField(field);
        setSortDirection(direction);
        sortData(items, field, direction);
    };

    const sortData = (data: Video[], field: keyof Video, direction: "asc" | "desc") => {
        const sortedData = [...data].sort((a, b) => {
            let aField = a[field];
            let bField = b[field];

            // If the field is "category", we'll sort by the name of the first category.
            if (field === "category" && Array.isArray(aField) && Array.isArray(bField)) {
                aField = (aField[0] as { id: string; name: string })?.name;
                bField = (bField[0] as { id: string; name: string })?.name;
            }

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

    const fields: (keyof Video)[] = [
        "title",
        "filename",
        ...(showStatus ? (["status"] as (keyof Video)[]) : []),
        "display_name",
        "start_download_at",
        "category",
    ];

    return (
        <div className="relative overflow-x-auto shadow-md sm:rounded-lg">
            <table className="w-full text-sm text-left text-gray-500 dark:text-gray-400">
                <thead className="text-xs text-gray-700 uppercase bg-gray-50 dark:bg-gray-700 dark:text-gray-400">
                    <tr>
                        {showCheckbox && (
                            <th scope="col" className="p-0">
                                <Checkbox
                                    id="checkbox-all-search"
                                    checked={selectAll}
                                    onChange={handleSelectAllChange}
                                />
                            </th>
                        )}
                        {showId && (
                            <th scope="col" className="px-6 py-3">
                                {t("ID")}
                            </th>
                        )}
                        {fields.map((field, index) => (
                            <th key={index} scope="col" className="px-6 py-3">
                                <div className="flex items-center">
                                    {t(capitalizeFirstLetter(field).replaceAll("_", " "))}
                                    <Icon onClick={() => handleSort(field)} />
                                </div>
                            </th>
                        ))}
                        {showEdit && (
                            <th scope="col" className="px-6 py-3">
                                <div className="flex items-center">Edit</div>
                            </th>
                        )}
                    </tr>
                </thead>
                <tbody>
                    {items.map((video, idx) => (
                        <tr
                            key={idx}
                            className="bg-white border-b dark:bg-gray-800 dark:border-gray-700 hover:bg-gray-50 dark:hover:bg-gray-600">
                            {showCheckbox && (
                                <Checkbox
                                    id={`checkbox-table-search-${idx}`}
                                    checked={video.isChecked || false}
                                    onChange={(isChecked) => handleCheckboxChange(idx, isChecked)}
                                />
                            )}
                            {showId && (
                                <th
                                    scope="row"
                                    className="px-6 py-4 font-medium text-gray-900 whitespace-nowrap dark:text-white">
                                    {video.id}
                                </th>
                            )}
                            <td className="px-6 py-4" title={video.title[0]}>
                                {truncateString(video.title[0], 40)}
                            </td>
                            <td className="px-6 py-4" title={video.filename}>
                                {video.filename}
                            </td>
                            {showStatus && (
                                <td className="px-6 py-4" title={video.status}>
                                    {t(video.status)}
                                </td>
                            )}
                            <td className="px-6 py-4" title={video.display_name}>
                                {video.display_name}
                            </td>
                            <td className="px-6 py-4" title={formatDate(video.start_download_at, "Europe/Paris")}>
                                {formatDate(video.start_download_at, "Europe/Paris")}
                            </td>
                            <td className="px-6 py-4" title={video.category[0].name}>
                                {video.category.map((cat) => (
                                    <span key={cat.id}>{cat.name}</span>
                                ))}
                            </td>
                            {showEdit && (
                                <td className="px-6 py-4">
                                    <a
                                        href="#"
                                        className="font-medium text-blue-600 dark:text-blue-500 hover:underline">
                                        Edit
                                    </a>
                                </td>
                            )}
                        </tr>
                    ))}
                </tbody>
            </table>
        </div>
    );
};

export default Table;
