import React from "react";
import { useParams } from "react-router-dom";
import { Category, CategoryDetailResponse, CompletedVideo } from "../../../type";
import VideoComponent from "../../../components/Media/Video";
import { toTitleCase } from "../../../utils/utils";
import { ApiRoutes, getApiRoute } from "../../../type/routes";
import { useTranslation } from "react-i18next";
import { useQuery } from "@tanstack/react-query";
import Container from "../../../components/Layout/Container";
import NotFound from "../../../components/Others/NotFound";

interface CategoryImageProps {
    category: Category;
    width: string;
    height: string;
}

const fetchImage = async (url: RequestInfo | URL): Promise<string> => {
    const response = await fetch(url, { credentials: "include" });
    const blob = await response.blob();
    return URL.createObjectURL(blob);
};

const fetchCategoryVideos = async (categoryId: string): Promise<CategoryDetailResponse> => {
    const url = getApiRoute(ApiRoutes.GET_VIDEO_BY_CATEGORY, "name", toTitleCase(categoryId));
    const response = await fetch(url, { credentials: "include" });
    if (!response.ok) {
        throw new Error(`HTTP error! status: ${response.status}`);
    }
    const { videos, category } = await response.json();

    const videosWithBlobUrls = await Promise.all(
        videos.map(async (video: CompletedVideo) => {
            const thumbnailUrl = getApiRoute(ApiRoutes.GET_VIDEO_THUMBNAIL_ID, "id", video.thumbnail);
            const imageUrl = await fetchImage(thumbnailUrl);
            return { ...video, thumbnail: imageUrl };
        })
    );

    return { category, videos: videosWithBlobUrls };
};

const CategoryDetailPage: React.FC = () => {
    const { t } = useTranslation();
    const { id } = useParams<{ id: string }>();

    if (!id) {
        throw new Error("No category ID provided");
    }

    const { data, isLoading, isError } = useQuery<CategoryDetailResponse, Error>({
        queryKey: ["categoryVideos", id],
        queryFn: () => fetchCategoryVideos(id),
        staleTime: 5 * 60 * 1000, // 5 minutes
        retry: false,
    });

    const CategoryImage = ({ category, width, height }: CategoryImageProps) => {
        const finalUrl = category.boxArtUrl?.replace("{width}", width).replace("{height}", height);
        return <img src={finalUrl} alt={category.name} className="hidden lg:block" />;
    };

    if (isLoading) {
        return <div>Loading...</div>;
    }

    if (!data || isError) {
        return (
            <Container>
                <div className="mt-14 flex flex-row items-baseline gap-3 p-4"></div>
                <div className="mt-5">
                    <h2 className="pb-5 text-2xl font-bold dark:text-stone-100">{t("Videos")}</h2>
                    <NotFound text={t("Category not found.")} />;
                </div>
            </Container>
        );
    }

    if (data && data.videos.length === 0) {
        return (
            <Container>
                <div className="mt-14 flex flex-row items-baseline gap-3 p-4">
                    <CategoryImage category={data.category} width="91" height="126" />
                    <h1 className="text-3xl font-bold dark:text-stone-100">{toTitleCase(id)}</h1>
                </div>
                <div className="mt-5">
                    <h2 className="pb-5 text-2xl font-bold dark:text-stone-100">{t("Videos")}</h2>
                    <NotFound text={t("No video found.")} />;
                </div>
            </Container>
        );
    }

    const { videos, category } = data;

    return (
        <Container>
            <div className="mt-14 flex flex-row items-baseline gap-3 p-4">
                <CategoryImage category={category} width="91" height="126" />
                <h1 className="text-3xl font-bold dark:text-stone-100">{toTitleCase(id)}</h1>
            </div>
            <div className="mt-5">
                <h2 className="pb-5 text-2xl font-bold dark:text-stone-100">{t("Videos")}</h2>
                <VideoComponent videos={videos} disablePicture={false} />
            </div>
        </Container>
    );
};

export default CategoryDetailPage;
