import { FC } from "react";

interface TitleProps {
    title: string;
}

const Title: FC<TitleProps> = ({ title }) => {
    return <h1 className="text-4xl font-bold dark:text-stone-100">{title}</h1>;
};

export default Title;
