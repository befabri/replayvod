import { useState } from "react";
import { ManageSchedule } from "../../type";
import { Link } from "react-router-dom";
import { Pathnames } from "../../type/routes";
import { qualityLabelToResolution } from "../../utils/utils";
import { Icon } from "@iconify/react/dist/iconify.js";

type PauseResumeManageSchedule = ManageSchedule & {
    isPaused: boolean;
};

const QueueCards = ({ items: initialItems }: any) => {
    const initializedItems = initialItems.map((item: any) => ({ ...item, isPaused: false }));
    const [items, setItems] = useState<PauseResumeManageSchedule[]>(initializedItems);

    const handleEdit = (field: any) => {
        console.log("Edit: ", field);
    };

    const handlePause = (field: any) => {
        console.log("Pause: ", field);
        setItems((prevItems) =>
            prevItems.map((item) => (item.id === field.id ? { ...item, isPaused: !item.isPaused } : item))
        );
    };

    return (
        <div className="grid grid-cols-1 gap-3 lg:grid-cols-2">
            {items.map((eventSub, idx) => (
                <div
                    key={idx}
                    className={`flex items-center gap-2 border-b bg-white p-3 hover:bg-gray-50 ${
                        eventSub.isPaused ? "dark:border-orange-500" : "dark:border-green-500"
                    } dark:bg-custom_lightblue dark:hover:bg-custom_lightblue`}>
                    <Link
                        to={`${Pathnames.Video.Channel}/${eventSub.channel.displayName.toLowerCase()}`}
                        className="flex-shrink-0">
                        <img
                            className="h-10 w-10 rounded-full"
                            src={eventSub.channel.profilePicture}
                            alt="Profile Picture"
                        />
                    </Link>
                    <span className="ms-4 min-w-0 flex-1 font-medium dark:text-white">
                        {eventSub.channel.broadcasterName}
                    </span>
                    <div className="inline-flex items-center gap-3">
                        <span className="mr-8 items-center rounded bg-gray-100 px-2.5 py-0 text-xs font-medium text-gray-800 dark:bg-gray-600 dark:text-white">
                            {qualityLabelToResolution(eventSub.quality)}p
                        </span>
                        <div className="flex gap-1">
                            <button
                                onClick={() => handlePause(eventSub)}
                                className="group flex w-full items-center rounded-lg p-1 text-gray-900 transition duration-75 hover:bg-gray-100 dark:text-white dark:hover:bg-custom_vista_blue"
                                aria-controls="pause">
                                {eventSub.isPaused ? (
                                    <Icon icon="material-symbols:play-arrow" width="18" height="18" />
                                ) : (
                                    <Icon icon="material-symbols:pause" width="18" height="18" />
                                )}
                            </button>
                            <button
                                onClick={() => handleEdit(eventSub)}
                                className="group flex w-full items-center rounded-lg p-1 text-gray-900 transition duration-75 hover:bg-gray-100 dark:text-white dark:hover:bg-custom_vista_blue"
                                aria-controls="edit">
                                <Icon icon="ant-design:tool-filled" width="18" height="18" />
                            </button>
                        </div>
                    </div>
                </div>
            ))}
        </div>
    );
};

export default QueueCards;
