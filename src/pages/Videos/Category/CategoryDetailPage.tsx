import React, { useState, useEffect } from "react";
import { useParams } from "react-router-dom";
import { Category, CompletedVideo } from "../../../type";
import VideoComponent from "../../../components/Media/Video";
import { toTitleCase } from "../../../utils/utils";
import { ApiRoutes, getApiRoute } from "../../../type/routes";
import { useTranslation } from "react-i18next";

interface CategoryImageProps {
    category: Category;
    width: string;
    height: string;
}

const CategoryDetailPage: React.FC = () => {
    const { t } = useTranslation();
    const { id } = useParams();
    const [videos, setVideos] = useState<CompletedVideo[]>([]);
    const [category, setCategory] = useState<Category>();
    const [isLoading, setIsLoading] = useState(true);

    const fetchImage = async (url: RequestInfo | URL) => {
        const response = await fetch(url, { credentials: "include" });
        const blob = await response.blob();
        return URL.createObjectURL(blob);
    };

    useEffect(() => {
        const fetchData = async () => {
            try {
                let url = getApiRoute(ApiRoutes.GET_VIDEO_BY_CATEGORY, "name", toTitleCase(id));

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

                const videosWithBlobUrls: CompletedVideo[] = await Promise.all(promises);
                setCategory(videosWithBlobUrls[0].videoCategory[0].category);
                setVideos(videosWithBlobUrls);
                setIsLoading(false);
            } catch (error) {
                console.error(`Error fetching data: ${error}`);
            }
        };

        fetchData();
    }, [id]);

    const CategoryImage = ({ category, width, height }: CategoryImageProps) => {
        const finalUrl = category.boxArtUrl?.replace("{width}", width).replace("{height}", height);
        return <img src={finalUrl} alt={category.name} className="hidden lg:block" />;
    };

    if (isLoading) {
        return;
    }
    // Add dumy image
    return (
        <div className="p-4 ">
            <div className="mt-14 flex flex-row items-baseline gap-3 p-4">
                <CategoryImage category={category} width="91" height="126" />
                <h1 className="text-3xl font-bold dark:text-stone-100">{toTitleCase(id)}</h1>
            </div>
            <div className="mt-5">
                <h2 className="pb-5 text-2xl font-bold dark:text-stone-100">{t("Videos")}</h2>
                <VideoComponent videos={videos} disablePicture={false} />
            </div>
        </div>
    );
};

export default CategoryDetailPage;
