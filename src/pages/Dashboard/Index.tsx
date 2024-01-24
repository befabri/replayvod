import React from "react";
import { useTranslation } from "react-i18next";
import VideoStatistics from "./VideoStatistics";
import LastLive from "./LastLive";
import ScheduleStatistics from "./ScheduleStatistics";

const DashboardPage: React.FC = () => {
    const { t } = useTranslation();

    return (
        <div className="p-4">
            <div className="mt-14 p-4">
                <h1 className="pb-5 text-3xl font-bold dark:text-stone-100">{t("Dashboard")}</h1>
                <div className="mb-4 grid gap-4 lg:grid-cols-2 2xl:grid-cols-3">
                    <VideoStatistics />
                    <LastLive />
                    <ScheduleStatistics />
                </div>
            </div>
        </div>
    );
};

export default DashboardPage;
