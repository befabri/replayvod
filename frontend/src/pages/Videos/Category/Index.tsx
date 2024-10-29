import React from "react";
import { Category } from "../../../type";
import { ApiRoutes, getApiRoute } from "../../../type/routes";
import CategoryComponent from "../../../components/Media/Category";
import { useTranslation } from "react-i18next";
import { useQuery } from "@tanstack/react-query";
import TitledLayout from "../../../components/Layout/TitledLayout";

const fetchCategories = async (): Promise<Category[]> => {
    const url = getApiRoute(ApiRoutes.GET_VIDEO_CATEGORY_ALL_DONE);
    const response = await fetch(url, {
        credentials: "include",
    });
    if (!response.ok) {
        throw new Error(`HTTP error! status: ${response.status}`);
    }
    return response.json();
};

const CategoryPage: React.FC = () => {
    const { t } = useTranslation();

    const {
        data: categories,
        isLoading,
        isError,
        error,
    } = useQuery<Category[], Error>({
        queryKey: ["categories"],
        queryFn: fetchCategories,
        staleTime: 5 * 60 * 1000,
    });

    if (isLoading) {
        return <div>{t("Loading")}</div>;
    }

    if (isError) {
        const errorMessage = error instanceof Error ? error.message : "An error occurred";
        return <div>{errorMessage}</div>;
    }

    return (
        <TitledLayout title={t("Categories")}>
            <CategoryComponent categories={categories} />
        </TitledLayout>
    );
};

export default CategoryPage;
