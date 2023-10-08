import { FC, useState, useEffect, useRef } from "react";
import { CompletedVideo } from "../../type";
import { Pathnames } from "../../type/routes";
import { toKebabCase, truncateString } from "../../utils/utils";

type VideoInfoProps = {
    video: CompletedVideo | undefined;
    disablePicture?: boolean;
};

const VideoInfoComponent: FC<VideoInfoProps> = ({ video, disablePicture = false }) => {
    const [divWidth, setDivWidth] = useState<number>(0);
    const divRef = useRef<HTMLDivElement>(null);

    let numberOfTagsToRender = Math.max(Math.floor(divWidth / 100), 2);

    useEffect(() => {
        const handleResize = () => {
            if (divRef.current) {
                setDivWidth(divRef.current.offsetWidth);
            }
        };

        handleResize();

        window.addEventListener("resize", handleResize);

        return () => {
            window.removeEventListener("resize", handleResize);
        };
    }, []);

    return (
        <div className="flex flex-row items-center gap-5 mt-2" ref={divRef}>
            {!disablePicture && (
                <a href={`${Pathnames.Channel}${video?.displayName.toLowerCase()}`}>
                    <img
                        className="w-12 h-12 min-w-[40px] rounded-full ml-2"
                        src={video?.channel.profilePicture}
                        alt="Profile Picture"
                    />
                </a>
            )}
            <div>
                <a href={`${Pathnames.Watch}${video?.id}`}>
                    <h3 className="text-base font-semibold dark:text-stone-100 hover:text-sky-500 dark:hover:text-sky-500">
                        {video?.titles[0].title.name}
                    </h3>
                </a>
                <a href={`${Pathnames.Channel}${video?.displayName.toLowerCase()}`}>
                    <h2 className="text-sm dark:text-stone-100 hover:text-sky-500 dark:hover:text-sky-500">
                        {video?.channel.broadcasterName}
                    </h2>
                </a>

                {video?.videoCategory.map((item) => (
                    <a href={`${Pathnames.Vod}/${toKebabCase(item.category.name)}`} key={item.categoryId}>
                        <p className="text-sm dark:text-violet-400 hover:text-violet-500 dark:hover:text-violet-500">
                            {item.category.name}
                        </p>
                    </a>
                ))}
                {video?.tags.slice(0, numberOfTagsToRender).map((item) => (
                    <span
                        className="bg-gray-100 text-gray-800 text-xs font-medium mr-2 px-2.5 py-0.5 rounded-full dark:bg-gray-700 dark:text-gray-300"
                        key={item.tag.name}>
                        {truncateString(item.tag.name, 18, false)}
                    </span>
                ))}
            </div>
        </div>
    );
};

export default VideoInfoComponent;
