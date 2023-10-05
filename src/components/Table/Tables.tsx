import { useState } from "react";
import Icon from "../UI/Icon/IconSort";
import Checkbox from "./CheckBoxTable";
import { Video, TableProps } from "../../type";
import { useTranslation } from "react-i18next";
import { capitalizeFirstLetter, toKebabCase, formatDate, truncateString } from "../../utils/utils";
import { Pathnames } from "../../type/routes";

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

    const storedTimeZone = localStorage.getItem("timeZone") || "Europe/London";

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

            if (field === "videoCategory" && Array.isArray(aField) && Array.isArray(bField)) {
                if (aField[0] && "category" in aField[0]) {
                    aField = aField[0].category.name || "";
                } else {
                    aField = "";
                }

                if (bField[0] && "category" in bField[0]) {
                    bField = bField[0].category.name || "";
                } else {
                    bField = "";
                }
            }

            if (field === "titles" && Array.isArray(aField) && Array.isArray(bField)) {
                if (aField[0] && "title" in aField[0]) {
                    aField = aField[0].title.name || "";
                } else {
                    aField = "";
                }

                if (bField[0] && "title" in bField[0]) {
                    bField = bField[0].title.name || "";
                } else {
                    bField = "";
                }
            }

            if (aField === undefined || bField === undefined) return 0;
            if (aField === null || bField === null) return 0;
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
        "titles",
        "filename",
        ...(showStatus ? (["status"] as (keyof Video)[]) : []),
        "displayName",
        "startDownloadAt",
        "videoCategory",
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
                        {fields.map((field) => (
                            <th key={field} scope="col" className="px-6 py-3">
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
                            key={video.id}
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
                            <td className="px-6 py-4" title={video.titles[0].title.name}>
                                {video.status !== "DONE" ? (
                                    truncateString(video.titles[0].title.name, 40)
                                ) : (
                                    <a href={`${Pathnames.Watch}${video.id}`}>
                                        {truncateString(video.titles[0].title.name, 40)}
                                    </a>
                                )}
                            </td>

                            <td className="px-6 py-4" title={video.filename}>
                                {video.filename}
                            </td>
                            {showStatus && (
                                <td className="px-6 py-4" title={video.status}>
                                    {t(video.status)}
                                </td>
                            )}
                            <td className="px-6 py-4" title={video.displayName}>
                                <a href={`${Pathnames.Channel}${video?.displayName.toLowerCase()}`}>
                                    {video.displayName}
                                </a>
                            </td>
                            <td className="px-6 py-4" title={formatDate(video.startDownloadAt, storedTimeZone)}>
                                {formatDate(video.startDownloadAt, storedTimeZone)}
                            </td>
                            <td className="px-6 py-4" title={video.videoCategory[0].category.name}>
                                {video.videoCategory.map((cat) => (
                                    <a
                                        key={cat.categoryId}
                                        href={`${Pathnames.Vod}/${toKebabCase(cat.category.name)}`}>
                                        <span key={cat.categoryId}>{cat.category.name}</span>
                                    </a>
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
