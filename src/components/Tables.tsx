import { useState } from "react";
import Icon from "./IconSort";
import Checkbox from "./checkboxProps";
import { Video, TableProps } from "../type";

type ExtendedTableProps = {
  showEdit?: boolean;
  showCheckbox?: boolean;
} & TableProps;

const Table = ({ items: initialItems, showEdit = true, showCheckbox = true }: ExtendedTableProps) => {
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
    console.log("event");
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
      const aField = a[field];
      const bField = b[field];

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

  const fields: (keyof Video)[] = ["id", "filename", "status", "display_name", "start_download_at", "Category"];

  return (
    <div className="relative overflow-x-auto shadow-md sm:rounded-lg">
      <table className="w-full text-sm text-left text-gray-500 dark:text-gray-400">
        <thead className="text-xs text-gray-700 uppercase bg-gray-50 dark:bg-gray-700 dark:text-gray-400">
          <tr>
            {showCheckbox && (
              <th scope="col" className="p-0">
                <Checkbox id="checkbox-all-search" checked={selectAll} onChange={handleSelectAllChange} />
              </th>
            )}
            {fields.map((field, index) => (
              <th key={index} scope="col" className="px-6 py-3">
                <div className="flex items-center">
                  {field}
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
              className="bg-white border-b dark:bg-gray-800 dark:border-gray-700 hover:bg-gray-50 dark:hover:bg-gray-600"
            >
              {showCheckbox && (
                <Checkbox
                  id={`checkbox-table-search-${idx}`}
                  checked={video.isChecked || false}
                  onChange={(isChecked) => handleCheckboxChange(idx, isChecked)}
                />
              )}
              <th scope="row" className="px-6 py-4 font-medium text-gray-900 whitespace-nowrap dark:text-white">
                {video.id}
              </th>
              <td className="px-6 py-4">{video.filename}</td>
              <td className="px-6 py-4">{video.status}</td>
              <td className="px-6 py-4">{video.display_name}</td>
              <td className="px-6 py-4">{video.start_download_at}</td>
              <td className="px-6 py-4">
                {video.category.map((cat) => (
                  <span key={cat.id}>{cat.name}</span>
                ))}
              </td>
              {showEdit && (
                <td className="px-6 py-4">
                  <a href="#" className="font-medium text-blue-600 dark:text-blue-500 hover:underline">
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
