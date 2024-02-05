import { useState } from "react";
import Icon from "../UI/Icon/IconSort";
import Checkbox from "./CheckBoxTable";
import { Video, TableProps, Category } from "../../type";
import { useTranslation } from "react-i18next";
import { toKebabCase, formatDate, truncateString } from "../../utils/utils";
import { Pathnames } from "../../type/routes";
import HrefLink from "../UI/Navigation/HrefLink";
import React from "react";
import ProfileImage from "../Profile/ProfileImage";

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
                aField = aField.length > 0 ? (aField[0] as Category).name : "";
                bField = bField.length > 0 ? (bField[0] as Category).name : "";
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

    const tableFieldDisplayNames: { [key in keyof Video]?: string } = {
        displayName: "Channel",
        videoCategory: "Category",
        titles: "Title",
        filename: "Filename",
        status: "Status",
        startDownloadAt: "Download At",
    };

    const fields: (keyof Video)[] = [
        "displayName",
        "videoCategory",
        "titles",
        "filename",
        ...(showStatus ? (["status"] as (keyof Video)[]) : []),
        "startDownloadAt",
    ];

    return (
        <div className="relative overflow-x-auto shadow-md sm:rounded-lg">
            <table className="w-full text-left text-sm text-gray-500 dark:text-gray-400">
                <thead className="bg-gray-50 uppercase text-gray-700 dark:bg-custom_lightblue dark:text-gray-400">
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
                                    {tableFieldDisplayNames[field]}
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
                            className="border-b bg-white hover:bg-gray-50 dark:border-custom_lightblue dark:bg-custom_blue dark:hover:bg-custom_lightblue">
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
                                    className="whitespace-nowrap px-6 py-4 font-medium text-gray-900 dark:text-white">
                                    {video.id}
                                </th>
                            )}
                            <td className="px-6 py-2.5" title={video.displayName}>
                                <div className="flex items-center gap-4">
                                    <div className="h-10 w-10">
                                        <HrefLink
                                            to={`${Pathnames.Video.Channel}/${video?.displayName.toLowerCase()}`}>
                                            <ProfileImage
                                                url={video.channel.profilePicture}
                                                height={"10"}
                                                width={"10"}
                                            />
                                        </HrefLink>
                                    </div>
                                    <HrefLink
                                        to={`${Pathnames.Video.Channel}/${video?.displayName.toLowerCase()}`}>
                                        {video.displayName}
                                    </HrefLink>
                                </div>
                            </td>
                            <td className="px-6 py-2.5" title={video.videoCategory[0].name}>
                                <div className="flex flex-row items-center gap-2">
                                    {video.videoCategory.map((cat, index) => (
                                        <React.Fragment key={cat.id}>
                                            <HrefLink to={`${Pathnames.Video.Category}/${toKebabCase(cat.name)}`}>
                                                <span>{cat.name}</span>
                                            </HrefLink>
                                            {index < video.videoCategory.length - 1 && <span> - </span>}
                                        </React.Fragment>
                                    ))}
                                </div>
                            </td>
                            <td className="px-6 py-2.5" title={video.titles[0]}>
                                {video.status !== "DONE" ? (
                                    truncateString(video.titles[0], 40)
                                ) : (
                                    <HrefLink to={`${Pathnames.Watch}${video.id}`}>
                                        {truncateString(video.titles[0], 40)}
                                    </HrefLink>
                                )}
                            </td>

                            <td className="px-6 py-2.5" title={video.filename}>
                                {video.filename}
                            </td>
                            {showStatus && (
                                <td className="px-6 py-2.5" title={video.status}>
                                    {t(video.status)}
                                </td>
                            )}

                            <td className="px-6 py-2.5" title={formatDate(video.startDownloadAt, storedTimeZone)}>
                                {formatDate(video.startDownloadAt, storedTimeZone)}
                            </td>
                            {showEdit && (
                                <td className="px-6 py-2.5">
                                    <a
                                        href="#"
                                        className="font-medium text-blue-600 hover:underline dark:text-blue-500">
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
