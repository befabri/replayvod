import { FC, useRef } from "react";
import { CompletedVideo } from "../../type";
import { Pathnames } from "../../type/routes";
import VideoInfoComponent from "./VideoInfo";
import { formatDate, formatDuration } from "../../utils/utils";
import { Link } from "react-router-dom";

type VideoProps = {
    videos: CompletedVideo[] | undefined;
    disablePicture?: boolean;
};

const VideoComponent: FC<VideoProps> = ({ videos, disablePicture = false }) => {
    const divRef = useRef<HTMLDivElement>(null);
    const hasOneOrTwoVideos = videos?.length === 1 || videos?.length === 2;
    const storedTimeZone = localStorage.getItem("timeZone") || "Europe/London";

    return (
        <div className="mb-4 grid grid-cols-1 md:grid-cols-[repeat(auto-fit,minmax(400px,1fr))] gap-3">
            {videos?.map((video) => (
                <div className="w-full" key={video.id} ref={divRef}>
                    <div className="relative border-4 border-custom_black hover:border-custom_vista_blue">
                        <Link to={`${Pathnames.Watch}${video.id}`}>
                            <img src={`${video.thumbnail}`} alt={`${video.displayName}`} />
                            <div className="absolute top-2 left-3 bg-black bg-opacity-50 text-stone-100">{`${formatDuration(
                                video.duration
                            )}`}</div>
                            <div className="absolute bottom-2 right-3 bg-black bg-opacity-60 text-stone-100">{`${formatDate(
                                video.downloadedAt,
                                storedTimeZone,
                                false
                            )}`}</div>
                        </Link>
                    </div>
                    <div className="flex justify-between">
                        <VideoInfoComponent video={video} disablePicture={disablePicture} />
                    </div>
                </div>
            ))}
            {hasOneOrTwoVideos && (
                <div className="w-full opacity-0">
                    <a href="#">
                        <img alt="dummy" />
                    </a>
                </div>
            )}
            {videos?.length === 1 && (
                <div className="w-full opacity-0">
                    <a href="#">
                        <img alt="dummy" />
                    </a>
                </div>
            )}
        </div>
    );
};

export default VideoComponent;
