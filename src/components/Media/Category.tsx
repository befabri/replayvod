import { FC, useRef } from "react";
import { Pathnames } from "../../type/routes";
import { Link } from "react-router-dom";
import { Category } from "../../type";
import { toKebabCase, truncateString } from "../../utils/utils";

type CategoryProps = {
    categories: Category[] | undefined;
};

interface CategoryImageProps {
    category: Category;
    width: string;
    height: string;
}

const CategoryComponent: FC<CategoryProps> = ({ categories }) => {
    const divRef = useRef<HTMLDivElement>(null);
    const hasOneOrTwoCategories = categories?.length === 1 || categories?.length === 2;

    const CategoryImage = ({ category, width, height }: CategoryImageProps) => {
        const finalUrl = category.boxArtUrl?.replace("{width}", width).replace("{height}", height);
        return <img src={finalUrl} alt={category.name} />;
    };

    return (
        <div className="mb-4 grid grid-cols-2 md:grid-cols-[repeat(auto-fit,minmax(190px,1fr))] gap-5">
            {categories?.map((category) => (
                <div className="w-full" key={category.id} ref={divRef}>
                    <div className="relative">
                        <Link to={`${Pathnames.Video.Category}/${toKebabCase(category.name)}`}>
                            <CategoryImage category={category} width="182" height="252" />
                        </Link>
                    </div>
                    <div className="flex flex-row">
                        <Link to={`${Pathnames.Video.Category}/${toKebabCase(category.name)}`}>
                            <h3 className="text-base font-semibold dark:text-stone-100 hover:text-custom_twitch dark:hover:text-custom_twitch">
                                {truncateString(category.name, 20, true)}
                            </h3>
                        </Link>
                    </div>
                </div>
            ))}
            {hasOneOrTwoCategories && (
                <div className="w-full opacity-0">
                    <a href="#">
                        <img alt="dummy" />
                    </a>
                </div>
            )}
            {categories?.length === 1 && (
                <div className="w-full opacity-0">
                    <a href="#">
                        <img alt="dummy" />
                    </a>
                </div>
            )}
        </div>
    );
};

export default CategoryComponent;
