// components/TableTasks.tsx
import { useState } from "react";
import { Task } from "../type";
import { Icon } from "@iconify/react";

const TableTasks: React.FC<{ items: Task[] }> = ({ items: initialItems }) => {
  const [items, setItems] = useState<Task[]>(initialItems);
  const ROOT_URL = import.meta.env.VITE_ROOTURL;

  const fetchTaskData = async (id: string) => {
    const response = await fetch(`${ROOT_URL}/api/schedule/tasks/run/${id}`, {
      credentials: "include",
    });
    const data = await response.json();
    const updatedItems = items.map((item) => (item.id === id ? data : item));
    setItems(updatedItems);
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
              <td className="px-6 py-4 whitespace-nowrap">{item.name}</td>
              <td className="px-6 py-4 whitespace-nowrap">{item.interval}</td>
              <td className="px-6 py-4 whitespace-nowrap">{new Date(item.lastExecution).toLocaleString()}</td>
              <td className="px-6 py-4 whitespace-nowrap">{item.lastDuration}</td>
              <td className="px-6 py-4 whitespace-nowrap">{new Date(item.nextExecution).toLocaleString()}</td>
              <td className="px-6 py-4 whitespace-nowrap">
                <button
                  onClick={() => fetchTaskData(item.id)}
                  className="p-1 rounded hover:bg-gray-200 dark:hover:bg-gray-700"
                >
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
