import React, { useState, useEffect } from "react";
import { Category } from "../../../type";
import { ApiRoutes, getApiRoute } from "../../../type/routes";
import CategoryComponent from "../../../components/Media/Category";
import { useTranslation } from "react-i18next";

const CategoryPage: React.FC = () => {
    const { t } = useTranslation();
    const [categories, setCategories] = useState<Category[]>([]);
    const [isLoading, setIsLoading] = useState(true);

    useEffect(() => {
        const fetchData = async () => {
            const url = getApiRoute(ApiRoutes.GET_VIDEO_CATEGORY_ALL_DONE);
            const response = await fetch(url, {
                credentials: "include",
            });
            if (!response.ok) {
                throw new Error(`HTTP error! status: ${response.status}`);
            }
            const data = await response.json();
            setCategories(data);
            setIsLoading(false);
        };

        fetchData();
        const intervalId = setInterval(fetchData, 10000);

        return () => clearInterval(intervalId);
    }, []);

    return (
        <div className="p-4 ">
            <div className="p-4 mt-14">
                <h1 className="text-3xl font-bold pb-5 dark:text-stone-100">{t("Categories")}</h1>
            </div>
            {isLoading ? <div>{t("Loading")}</div> : <CategoryComponent categories={categories} />}
        </div>
    );
};

export default CategoryPage;
