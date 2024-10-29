import { FC, useRef } from "react";
import { Pathnames } from "../../type/routes";
import { Link } from "react-router-dom";
import { Category } from "../../type";
import { toKebabCase, truncateString } from "../../utils/utils";
import HrefLink from "../UI/Navigation/HrefLink";

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
        return (
            <img
                src={finalUrl}
                alt={category.name}
                className="w-full border-4 border-custom_black hover:border-custom_vista_blue"
            />
        );
    };

    return (
        <div className="mb-4 grid grid-cols-[repeat(auto-fit,minmax(160px,1fr))] gap-3">
            {categories?.map((category) => (
                <div key={category.id} ref={divRef}>
                    <div>
                        <Link to={`${Pathnames.Video.Category}/${toKebabCase(category.name)}`}>
                            <CategoryImage category={category} width="182" height="252" />
                        </Link>
                    </div>
                    <div className="flex flex-row">
                        <HrefLink to={`${Pathnames.Video.Category}/${toKebabCase(category.name)}`} style="title">
                            {truncateString(category.name, 20, true)}
                        </HrefLink>
                    </div>
                </div>
            ))}
            {hasOneOrTwoCategories && <></>}
            {categories?.length === 1 && (
                <>
                    <div className="w-full opacity-0">
                        <a href="#">
                            <img alt="dummy" />
                        </a>
                    </div>
                </>
            )}
        </div>
    );
};

export default CategoryComponent;
