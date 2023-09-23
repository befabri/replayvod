import React, { useState, useEffect } from "react";
import { useParams } from "react-router-dom";
import { useTranslation } from "react-i18next";
import { CompletedVideo } from "../type";
import VideoComponent from "../components/Video";
import { toKebabCase, toTitleCase } from "../utils/utils";

const VodCategory: React.FC = () => {
    const { t } = useTranslation();
    const { id } = useParams();
    const [videos, setVideos] = useState<CompletedVideo[]>([]);
    const [isLoading, setIsLoading] = useState(true);
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
                const filteredVideos = videosWithBlobUrls.filter((video) =>
                    video.videoCategory.some(
                        (cat: { category: { name: string } }) => toKebabCase(cat.category.name) === id
                    )
                );
                setVideos(filteredVideos);
                setIsLoading(false);
            })
            .catch((error) => {
                console.error(`Error fetching data: ${error}`);
            });
    }, []);

    if (isLoading) {
        return;
    }
    return (
        <div className="p-4 ">
            <div className="p-4 mt-14">
                <h1 className="text-3xl font-bold pb-5 dark:text-stone-100">{toTitleCase(id)}</h1>
            </div>
            <VideoComponent videos={videos} disablePicture={true} />
        </div>
    );
};

export default VodCategory;
