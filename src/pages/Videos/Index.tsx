import React, { useState, useEffect } from "react";
import { useTranslation } from "react-i18next";
import { CompletedVideo } from "../../type";
import DropdownButton from "../../components/UI/Button/ButtonDropdown";
import Table from "../../components/Table/Tables";
import VideoComponent from "../../components/Media/Video";
import { ApiRoutes, getApiRoute } from "../../type/routes";
import { useLocation, useNavigate } from "react-router-dom";

const VideosPage: React.FC = () => {
    const { t } = useTranslation();
    const location = useLocation();
    const [videos, setVideos] = useState<CompletedVideo[]>([]);
    const [isLoading, setIsLoading] = useState(true);
    const [view, setView] = useState({ value: "grid", label: "Grid" });
    const [selectedOrder, setSelectedOrder] = useState({ value: "recently", label: t("Recently Added") });
    const navigate = useNavigate();

    const fetchImage = async (url: RequestInfo | URL) => {
        const response = await fetch(url, { credentials: "include" });
        const blob = await response.blob();
        return URL.createObjectURL(blob);
    };

    useEffect(() => {
        const fetchData = async () => {
            try {
                let url = getApiRoute(ApiRoutes.GET_VIDEO_FINISHED);
                const response = await fetch(url, { credentials: "include" });

                if (!response.ok) {
                    throw new Error(`HTTP error! status: ${response.status}`);
                }

                const data = await response.json();
                const promises = data.map(async (video: CompletedVideo) => {
                    url = getApiRoute(ApiRoutes.GET_VIDEO_THUMBNAIL_ID, "id", video.thumbnail);
                    const imageUrl = await fetchImage(url);
                    return { ...video, thumbnail: imageUrl };
                });

                const videosWithBlobUrls = await Promise.all(promises);
                const params = new URLSearchParams(location.search);
                const sortParam = params.get("sort");
                const newOrder = orderOptions.find((option) => option.value === sortParam) || {
                    value: "recently",
                    label: t("Recently Added"),
                };
                setSelectedOrder(newOrder);
                sortVideos(videosWithBlobUrls, newOrder.value);
                setIsLoading(false);
            } catch (error) {
                console.error(`Error fetching data: ${error}`);
            }
        };

        fetchData();
    }, []);

    const orderOptions = [
        { value: "recently", label: t("Recently Added") },
        { value: "channel_asc", label: t("Channel (A-Z)") },
        { value: "channel_desc", label: t("Channel (Z-A)") },
    ];

    const viewOptions = [
        { value: "grid", label: t("Grid") },
        { value: "table", label: t("Table") },
    ];

    const sortVideos = (videos: CompletedVideo[], sortOrder: string) => {
        const sortedVideos = [...videos];
        if (sortOrder === "channel_asc") {
            sortedVideos.sort((a, b) => a.channel.broadcasterName.localeCompare(b.channel.broadcasterName));
        } else if (sortOrder === "channel_desc") {
            sortedVideos.sort((b, a) => a.channel.broadcasterName.localeCompare(b.channel.broadcasterName));
        } else {
            sortedVideos.sort((a, b) => new Date(b.downloadedAt).getTime() - new Date(a.downloadedAt).getTime());
        }
        setVideos(sortedVideos);
    };

    const handleOrderSelected = (value: string) => {
        const selectedOption = orderOptions.find((option) => option.value === value);
        if (selectedOption) {
            setSelectedOrder(selectedOption);
            sortVideos(videos, selectedOption.value);
            navigate(`${location.pathname}?sort=${value}`);
        }
    };

    const handleViewSelected = (value: string) => {
        const selectedOption = viewOptions.find((option) => option.value === value);
        if (selectedOption) {
            setView(selectedOption);
        }
    };

    if (isLoading) {
        return;
    }

    return (
        <div className="p-4">
            <div className="p-4 mt-14">
                <h1 className="text-3xl font-bold pb-5 dark:text-stone-100">{t("Videos")}</h1>
            </div>
            <div className="flex mb-4 items-center justify-end space-x-5">
                {view.value === "grid" && (
                    <div className="space-x-2">
                        <DropdownButton
                            label={selectedOrder.label}
                            options={orderOptions}
                            onOptionSelected={handleOrderSelected}
                        />
                    </div>
                )}
                <div className="space-x-2">
                    <DropdownButton
                        label={view.label}
                        options={viewOptions}
                        onOptionSelected={handleViewSelected}
                    />
                </div>
            </div>
            {view.value === "grid" ? (
                <VideoComponent videos={videos} disablePicture={false} />
            ) : (
                <div className="mt-4">
                    <Table
                        items={videos}
                        showEdit={false}
                        showCheckbox={false}
                        showId={false}
                        showStatus={false}
                    />
                </div>
            )}
        </div>
    );
};

export default VideosPage;
