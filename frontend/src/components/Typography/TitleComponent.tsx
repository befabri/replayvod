import React from "react";

interface TitleProps extends React.ComponentPropsWithoutRef<"h1"> {
    title: string;
}

const Title = ({ title, ...props }: TitleProps) => {
    return (
        <h1 {...props} className={`text-4xl font-bold dark:text-stone-100 ${props.className || ""}`}>
            {title}
        </h1>
    );
};

export default Title;
