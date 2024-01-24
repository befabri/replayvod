import React, { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import { ManageSchedule } from "../../type";
import { ApiRoutes, Pathnames, getApiRoute } from "../../type/routes";
import { Icon } from "@iconify/react/dist/iconify.js";
import { qualityLabelToResolution } from "../../utils/utils";
import HrefLink from "../../components/UI/Navigation/HrefLink";
import { Link } from "react-router-dom";
import ScheduleModal from "../../components/UI/Modal/ScheduleModal";
import NotFound from "../../components/Others/NotFound";

const ManagePage: React.FC = () => {
    const { t } = useTranslation();
    const [schedules, setSchedules] = useState<ManageSchedule[]>([]);
    const [isLoading, setIsLoading] = useState(true);
    const [isModalOpen, setModalOpen] = useState(false);
    const [selectedSchedule, setSelectedSchedule] = useState<ManageSchedule | null>(null);

    useEffect(() => {
        const fetchData = async () => {
            const url = getApiRoute(ApiRoutes.GET_SCHEDULE);
            const response = await fetch(url, {
                credentials: "include",
            });
            if (!response.ok) {
                throw new Error(`HTTP error! status: ${response.status}`);
            }
            const data = await response.json();
            const initializedItems = data.map((item: any) => ({ ...item, isPaused: false }));
            setSchedules(initializedItems || []);
            setIsLoading(false);
        };

        fetchData();
    }, []);

    const postData = async ({ id, enable }: { id: number; enable: boolean }) => {
        try {
            const url = getApiRoute(ApiRoutes.POST_TOGGLE_SCHEDULE, "id", id);
            const response = await fetch(url, {
                method: "POST",
                credentials: "include",
                headers: {
                    "Content-Type": "application/json",
                },
                body: JSON.stringify({ enable }),
            });

            if (!response.ok) {
                throw new Error(`HTTP error! status: ${response.status}`);
            }
            const responseData = await response.json();
            return responseData;
        } catch (error) {
            console.error(`Error posting data: ${error}`);
        }
    };

    const handlePause = async (field: ManageSchedule) => {
        try {
            const response = await postData({ id: field.id, enable: !field.isDisabled });
            if (response && response.message === "Schedule is already in the desired state") {
                return;
            }
            setSchedules((prevItems) =>
                prevItems.map((item) => (item.id === field.id ? { ...item, isDisabled: !item.isDisabled } : item))
            );
        } catch (error) {
            console.error(`Error handling pause: ${error}`);
        }
    };

    const handleEdit = (schedule: ManageSchedule) => {
        setSelectedSchedule(schedule);
        setModalOpen(true);
    };

    const handleScheduleDelete = (deletedScheduleId: number) => {
        setSchedules((prevSchedules) => prevSchedules.filter((schedule) => schedule.id !== deletedScheduleId));
    };

    if (isLoading) {
        return <div>{t("Loading")}</div>;
    }

    //TODO
    const handleDataChange = (newData: any) => {
        console.log("newData: ", newData);
        console.log("schedules: ", schedules);
    };

    const hasNoSchedules = schedules.length === 0;

    return (
        <div className="p-4">
            <div className="mt-14 p-4">
                <h1 className="pb-5 text-3xl font-bold dark:text-stone-100">{t("Manage Schedule")}</h1>
            </div>
            {hasNoSchedules && <NotFound text={t("No schedules available.")} />}
            <div className="grid grid-cols-1 gap-4 lg:grid-cols-2">
                {schedules.map((schedule, idx) => (
                    <div
                        key={idx}
                        className={`flex items-center gap-2 border-b-2 bg-white p-3 hover:bg-gray-50 ${
                            schedule.isDisabled ? "dark:border-orange-500" : "dark:border-green-500"
                        } dark:bg-custom_lightblue dark:hover:bg-custom_lightblue`}>
                        <Link
                            to={`${Pathnames.Video.Channel}/${schedule.channel.displayName.toLowerCase()}`}
                            className="flex-shrink-0">
                            <img
                                className="h-10 w-10 rounded-full"
                                src={schedule.channel.profilePicture}
                                alt="Profile Picture"
                            />
                        </Link>
                        <span className="ms-4 min-w-0 flex-1">
                            <HrefLink
                                to={`${Pathnames.Video.Channel}/${schedule.channel.displayName.toLowerCase()}`}
                                style="normal">
                                {schedule.channel.broadcasterName}
                            </HrefLink>
                        </span>
                        <div className="inline-flex items-center gap-3">
                            <span className="mr-8 items-center rounded bg-gray-100 px-2.5 py-0 text-xs font-medium text-gray-800 dark:bg-gray-600 dark:text-white">
                                {qualityLabelToResolution(schedule.quality)}p
                            </span>
                            <div className="flex gap-1">
                                <button
                                    onClick={() => handlePause(schedule)}
                                    className="group flex w-full items-center rounded-lg p-1 text-gray-900 transition duration-75 hover:bg-gray-100 dark:text-white dark:hover:bg-custom_vista_blue"
                                    aria-controls="pause">
                                    {schedule.isDisabled ? (
                                        <Icon icon="material-symbols:play-arrow" width="18" height="18" />
                                    ) : (
                                        <Icon icon="material-symbols:pause" width="18" height="18" />
                                    )}
                                </button>
                                <button
                                    onClick={() => handleEdit(schedule)}
                                    className="group flex w-full items-center rounded-lg p-1 text-gray-900 transition duration-75 hover:bg-gray-100 dark:text-white dark:hover:bg-custom_vista_blue"
                                    aria-controls="edit">
                                    <Icon icon="ant-design:tool-filled" width="18" height="18" />
                                </button>
                            </div>
                        </div>
                    </div>
                ))}
                {isModalOpen && selectedSchedule && (
                    <ScheduleModal
                        isOpen={isModalOpen}
                        onClose={() => {
                            setModalOpen(false);
                            setSelectedSchedule(null);
                        }}
                        onScheduleDelete={handleScheduleDelete}
                        data={selectedSchedule}
                        onDataChange={handleDataChange}
                    />
                )}
            </div>
        </div>
    );
};

export default ManagePage;
