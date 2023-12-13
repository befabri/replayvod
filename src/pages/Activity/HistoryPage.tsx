import React, { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import Table from "../../components/Table/Tables";
import { Video } from "../../type";
import { ApiRoutes, getApiRoute } from "../../type/routes";

const HistoryPage: React.FC = () => {
    const { t } = useTranslation();
    const [videos, setVideos] = useState<Video[]>([]);
    const [isLoading, setIsLoading] = useState(true);

    useEffect(() => {
        const fetchData = async () => {
            try {
                const url = getApiRoute(ApiRoutes.GET_VIDEO_ALL);
                const response = await fetch(url, { credentials: "include" });

                if (!response.ok) {
                    throw new Error(`HTTP error! status: ${response.status}`);
                }

                const data = await response.json();
                setVideos(data);
                setIsLoading(false);
            } catch (error) {
                console.error(`Error fetching data: ${error}`);
            }
        };

        fetchData();
    }, []);

    if (isLoading) {
        return <div>{t("Loading")}</div>;
    }

    return (
        <div className="p-4">
            <div className="p-4 mt-14">
                <h1 className="text-3xl font-bold pb-5 dark:text-stone-100">{t("History")}</h1>
            </div>
            {isLoading ? (
                <div>{t("Loading")}</div>
            ) : (
                <Table items={videos} showEdit={false} showCheckbox={false} showId={false} />
            )}
        </div>
    );
};

export default HistoryPage;
