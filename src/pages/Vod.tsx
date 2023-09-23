import React, { useState, useEffect } from "react";
import { useTranslation } from "react-i18next";
import { CompletedVideo } from "../type";
import DropdownButton from "../components/ButtonDropdown";
import Table from "../components/Tables";
import VideoComponent from "../components/Video";

const Vod: React.FC = () => {
    const { t } = useTranslation();
    const [videos, setVideos] = useState<CompletedVideo[]>([]);
    const [isLoading, setIsLoading] = useState(true);
    const [view, setView] = useState("Grille");
    const ROOT_URL = import.meta.env.VITE_ROOTURL;

    const fetchImage = async (url: RequestInfo | URL) => {
        const response = await fetch(url, { credentials: "include" });
        const blob = await response.blob();
        return URL.createObjectURL(blob);
    };

    useEffect(() => {
        fetch(`${ROOT_URL}/api/videos/finished`, {
            credentials: "include",
        })
            .then((response) => {
                if (!response.ok) {
                    throw new Error(`HTTP error! status: ${response.status}`);
                }
                return response.json();
            })
            .then(async (data) => {
                const promises = data.map(async (video: CompletedVideo) => {
                    const imageUrl = await fetchImage(`${ROOT_URL}/api/videos/thumbnail/${video.thumbnail}`);
                    return { ...video, thumbnail: imageUrl };
                });

                const videosWithBlobUrls = await Promise.all(promises);
                setVideos(videosWithBlobUrls);
                setIsLoading(false);
            })
            .catch((error) => {
                console.error(`Error fetching data: ${error}`);
            });
    }, []);

    const handleOptionSelected = (value: any) => {
        setView(value);
    };

    if (isLoading) {
        return;
    }

    return (
        <div className="p-4 ">
            <div className="p-4 mt-14">
                <h1 className="text-3xl font-bold pb-5 dark:text-stone-100">{t("Videos")}</h1>
            </div>
            <div className="flex mb-4 justify-end">
                <DropdownButton
                    label={t("View")}
                    options={[t("Grid"), t("Table")]}
                    onOptionSelected={handleOptionSelected}
                />
            </div>
            {view === t("Grid") ? (
                <VideoComponent videos={videos} disablePicture={true} />
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

export default Vod;
