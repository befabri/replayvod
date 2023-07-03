import React, { useState, useEffect } from "react";
import { useTranslation } from "react-i18next";
import { Video } from "../type";
import DropdownButton from "../components/ButtonDropdown";
import Table from "../components/Tables";

const Vod: React.FC = () => {
    const { t } = useTranslation();
    const [videos, setVideos] = useState<any[]>([]);
    const [isLoading, setIsLoading] = useState(true);
    const [view, setView] = useState("Table");
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
                const promises = data.map(async (video: Video) => {
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
        console.log(value);
        setView(value);
    };

    if (isLoading) {
        return;
    }

    return (
        <div className="p-4 sm:ml-64">
            <div className="p-4 mt-14">
                <h1 className="text-3xl font-bold pb-5 dark:text-stone-100">{t("Videos")}</h1>
            </div>
            <div className="flex mb-4 justify-end">
                <DropdownButton
                    label={t("View")}
                    options={[t("Table"), t("Grid")]}
                    onOptionSelected={handleOptionSelected}
                />
            </div>
            {view === t("Grid") ? (
                <div className="mb-4 grid grid-cols-1 md:grid-cols-[repeat(auto-fit,minmax(200px,1fr))] gap-5">
                    {videos.map((video) => (
                        <div key={video.id}>
                            <h2 className="text-1xl dark:text-stone-100">{video.display_name}</h2>
                            <p className="text-medium dark:text-stone-100">
                                {t("Size")}: {video.size}
                            </p>
                            <p className="text-medium dark:text-stone-100">
                                {t("Downloaded at")}: {video.downloaded_at}
                            </p>
                            <video controls poster={video.thumbnail} preload="none" className="w-full max-w-lg">
                                <source src={`${ROOT_URL}/api/videos/play/${video.id}`} type="video/mp4" />
                                {t("Your browser does not support the video tag.")}
                            </video>
                        </div>
                    ))}
                </div>
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
