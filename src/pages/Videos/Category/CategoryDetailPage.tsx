import React, { useState, useEffect } from "react";
import { useParams } from "react-router-dom";
import { CompletedVideo } from "../../../type";
import VideoComponent from "../../../components/Media/Video";
import { toKebabCase, toTitleCase } from "../../../utils/utils";
import { ApiRoutes, getApiRoute } from "../../../type/routes";

const CategoryDetailPage: React.FC = () => {
    const { id } = useParams();
    const [videos, setVideos] = useState<CompletedVideo[]>([]);
    const [isLoading, setIsLoading] = useState(true);

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
                const filteredVideos = videosWithBlobUrls.filter((video) =>
                    video.videoCategory.some(
                        (cat: { category: { name: string } }) => toKebabCase(cat.category.name) === id
                    )
                );

                setVideos(filteredVideos);
                setIsLoading(false);
            } catch (error) {
                console.error(`Error fetching data: ${error}`);
            }
        };

        fetchData();
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

export default CategoryDetailPage;
