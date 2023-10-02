import React, { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import Table from "../../components/Tables";
import { Video } from "../../type";
import { ApiRoutes, getApiRoute } from "../../type/routes";

const HistoryPage: React.FC = () => {
    const { t } = useTranslation();
    const [videos, setVideos] = useState<Video[]>([]);
    const [isLoading, setIsLoading] = useState(true);

    useEffect(() => {
        let url = getApiRoute(ApiRoutes.GET_VIDEO_ALL);
        fetch(url, {
            credentials: "include",
        })
            .then((response) => {
                if (!response.ok) {
                    throw new Error(`HTTP error! status: ${response.status}`);
                }
                return response.json();
            })
            .then((data) => {
                setVideos(data);

                setIsLoading(false);
            })
            .catch((error) => {
                console.error(`Error fetching data: ${error}`);
            });
    }, []);

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
