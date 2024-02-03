import React, { useState } from "react";
import { useTranslation } from "react-i18next";
import { Schedule } from "../../type";
import { ApiRoutes, Pathnames, getApiRoute } from "../../type/routes";
import { Icon } from "@iconify/react/dist/iconify.js";
import HrefLink from "../../components/UI/Navigation/HrefLink";
import { Link } from "react-router-dom";
import ScheduleModal from "../../components/UI/Modal/ScheduleModal";
import NotFound from "../../components/Others/NotFound";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import Badge from "../../components/UI/Badge/Badge";
import TitledLayout from "../../components/Layout/TitledLayout";

const ManagePage: React.FC = () => {
    const { t } = useTranslation();
    const [isModalOpen, setModalOpen] = useState(false);
    const [selectedSchedule, setSelectedSchedule] = useState<Schedule | null>(null);
    const queryClient = useQueryClient();

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
        return initializedItems;
    };

    const {
        data: schedules,
        isLoading: isLoading,
        isError: isError,
    } = useQuery<Schedule[], Error>({
        queryKey: ["schedules"],
        queryFn: fetchData,
        staleTime: 5 * 60 * 1000,
    });

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

    const handlePause = async (field: Schedule) => {
        try {
            const response = await postData({ id: field.id, enable: !field.isDisabled });
            if (response && response.message === "Schedule is already in the desired state") {
                return;
            }
            queryClient.setQueryData<Schedule[]>(
                ["schedules"],
                (oldSchedules) =>
                    oldSchedules?.map((schedule) =>
                        schedule.id === field.id ? { ...schedule, isDisabled: !schedule.isDisabled } : schedule
                    )
            );
        } catch (error) {
            console.error(`Error handling pause: ${error}`);
        }
    };

    const handleEdit = (schedule: Schedule) => {
        setSelectedSchedule(schedule);
        setModalOpen(true);
    };

    const handleScheduleDelete = (deletedScheduleId: number) => {
        queryClient.setQueryData<Schedule[]>(
            ["schedules"],
            (oldSchedules) => oldSchedules?.filter((schedule) => schedule.id !== deletedScheduleId)
        );
    };

    if (isLoading) {
        return (
            <div className="p-4">
                <div className="mt-14 p-4">
                    <h1 className="pb-5 text-3xl font-bold dark:text-stone-100">{t("Manage Schedule")}</h1>
                </div>
                <div>{t("Loading")}</div>;
            </div>
        );
    }

    if (!schedules || isError || schedules.length === 0) {
        return (
            <div className="p-4">
                <div className="mt-14 p-4">
                    <h1 className="pb-5 text-3xl font-bold dark:text-stone-100">{t("Manage Schedule")}</h1>
                </div>
                <NotFound text={t("No schedules available.")} />
            </div>
        );
    }

    return (
        <TitledLayout title={t("Manage Schedule")}>
            <div className="grid grid-cols-1 gap-3 lg:grid-cols-[repeat(auto-fit,minmax(600px,1fr))]">
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
                            {schedule.isDeleteRediff && (
                                <Badge color="teal" className="hidden md:flex">
                                    {schedule.timeBeforeDelete} min
                                </Badge>
                            )}
                            {schedule.hasMinView && (
                                <Badge color="indigo" className="hidden md:flex">
                                    {schedule.viewersCount} views
                                </Badge>
                            )}
                            {schedule.categories.length > 0 && (
                                <Badge color="green" className="hidden md:flex">
                                    {schedule.categories.length} categories
                                </Badge>
                            )}
                            {schedule.tags.length > 0 && (
                                <Badge color="purple" className="hidden md:flex">
                                    {schedule.tags.length} tags
                                </Badge>
                            )}
                            <Badge color="red">{schedule.quality}p</Badge>
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
                    />
                )}
            </div>
        </TitledLayout>
    );
};

export default ManagePage;
