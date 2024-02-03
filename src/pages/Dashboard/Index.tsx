import React from "react";
import { useTranslation } from "react-i18next";
import VideoStatistics from "./VideoStatistics";
import LastLive from "./LastLiveStatistics.tsx";
import ScheduleStatistics from "./ScheduleStatistics";
import TitledLayout from "../../components/Layout/TitledLayout.tsx";

const DashboardPage: React.FC = () => {
    const { t } = useTranslation();

    return (
        <TitledLayout title={t("Dashboard")}>
            <div className="mb-4 grid gap-4 lg:grid-cols-2 2xl:grid-cols-3">
                <VideoStatistics />
                <LastLive />
                <ScheduleStatistics />
            </div>
        </TitledLayout>
    );
};

export default DashboardPage;
