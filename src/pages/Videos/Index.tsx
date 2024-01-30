import React, { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import { CompletedVideo } from "../../type";
import DropdownButton from "../../components/UI/Button/ButtonDropdown";
import Table from "../../components/Table/Tables";
import VideoComponent from "../../components/Media/Video";
import { ApiRoutes, getApiRoute } from "../../type/routes";
import { useLocation, useNavigate } from "react-router-dom";
import { useQuery } from "@tanstack/react-query";
import Container from "../../components/Layout/Container";

const fetchImage = async (url: string | URL | Request) => {
    const response = await fetch(url, { credentials: "include" });
    const blob = await response.blob();
    return URL.createObjectURL(blob);
};

const fetchData = async () => {
    const url = getApiRoute(ApiRoutes.GET_VIDEO_FINISHED);
    const response = await fetch(url, { credentials: "include" });

    if (!response.ok) {
        throw new Error(`HTTP error! status: ${response.status}`);
    }

    const data = await response.json();
    const promises = data.map(async (video: { thumbnail: string | number | undefined }) => {
        const imageUrl = await fetchImage(getApiRoute(ApiRoutes.GET_VIDEO_THUMBNAIL_ID, "id", video.thumbnail));
        return { ...video, thumbnail: imageUrl };
    });

    return Promise.all(promises);
};

const VideosPage: React.FC = () => {
    const { t } = useTranslation();
    const location = useLocation();
    const [view, setView] = useState({ value: "grid", label: "Grid" });
    const [selectedOrder, setSelectedOrder] = useState({ value: "recently", label: t("Recently Added") });
    const [sortedVideos, setSortedVideos] = useState<CompletedVideo[]>([]);
    const navigate = useNavigate();

    const { data: videos, isLoading } = useQuery<CompletedVideo[], Error>({
        queryKey: ["videos"],
        queryFn: fetchData,
        staleTime: 5 * 60 * 1000,
    });

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
            sortedVideos.sort((a, b) => b.channel.broadcasterName.localeCompare(a.channel.broadcasterName));
        } else {
            sortedVideos.sort((a, b) => new Date(b.downloadedAt).getTime() - new Date(a.downloadedAt).getTime());
        }
        return sortedVideos;
    };

    useEffect(() => {
        if (videos) {
            setSortedVideos(sortVideos(videos, selectedOrder.value));
        }
    }, [videos, selectedOrder]);

    const handleOrderSelected = (value: string) => {
        const selectedOption = orderOptions.find((option) => option.value === value);
        if (selectedOption) {
            setSelectedOrder(selectedOption);
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
        <Container>
            <div className="mt-14 p-4">
                <h1 className="pb-5 text-3xl font-bold dark:text-stone-100">{t("Videos")}</h1>
            </div>
            <div className="mb-4 flex items-center justify-end space-x-5">
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
                <VideoComponent videos={sortedVideos} disablePicture={false} />
            ) : (
                <div className="mt-4">
                    <Table
                        items={sortedVideos}
                        showEdit={false}
                        showCheckbox={false}
                        showId={false}
                        showStatus={false}
                    />
                </div>
            )}
        </Container>
    );
};

export default VideosPage;
