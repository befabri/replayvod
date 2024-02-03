import React, { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import { ApiRoutes, Pathnames } from "../../../type/routes";
import { Link, useLocation, useNavigate } from "react-router-dom";
import { Channel, Stream } from "../../../type";
import DropdownButton from "../../../components/UI/Button/ButtonDropdown";
import { useQuery } from "@tanstack/react-query";
import Container from "../../../components/Layout/Container";
import { customFetch } from "../../../utils/utils";

const ChannelPage: React.FC = () => {
    const { t } = useTranslation();
    const location = useLocation();
    const navigate = useNavigate();
    const [_selectedOrder, setSelectedOrder] = useState({ value: "channel_asc", label: t("Order") });
    const [sortedChannels, setSortedChannels] = useState<Channel[]>([]);

    const {
        data: channels,
        isLoading: isLoadingChannels,
        isError: isErrorChannels,
        error: errorChannels,
    } = useQuery<Channel[], Error>({
        queryKey: ["channels"],
        queryFn: (): Promise<Channel[]> => customFetch(ApiRoutes.GET_USER_FOLLOWED_CHANNELS),
        staleTime: 5 * 60 * 1000,
    });

    const {
        data: streams,
        isLoading: isLoadingStreams,
        isError: isErrorStreams,
        error: errorStreams,
    } = useQuery<Stream[], Error>({
        queryKey: ["streams"],
        queryFn: (): Promise<Stream[]> => customFetch(ApiRoutes.GET_USER_FOLLOWED_STREAMS),
        staleTime: 5 * 60 * 1000,
    });

    const isLoading = isLoadingChannels;
    const isError = isErrorChannels || isErrorStreams;
    const error = errorChannels || errorStreams;

    const orderOptions = [
        { value: "channel_asc", label: t("Channel (A-Z)"), icon: "mdi:sort-alphabetical-ascending" },
        { value: "channel_desc", label: t("Channel (Z-A)"), icon: "mdi:sort-alphabetical-descending" },
    ];

    const sortChannels = (channels: Channel[], sortOrder: string) => {
        return [...channels].sort((a, b) => {
            return sortOrder === "channel_desc"
                ? b.broadcasterName.localeCompare(a.broadcasterName)
                : a.broadcasterName.localeCompare(b.broadcasterName);
        });
    };

    useEffect(() => {
        const params = new URLSearchParams(location.search);
        const sortParam = params.get("sort");
        const newOrder = orderOptions.find((option) => option.value === sortParam) || orderOptions[0];
        setSelectedOrder(newOrder);

        if (channels) {
            setSortedChannels(sortChannels(channels, newOrder.value));
        }
        // orderOptions
        // eslint-disable-next-line react-hooks/exhaustive-deps
    }, [channels, location.search]);

    const handleOrderSelected = (value: string) => {
        const selectedOption = orderOptions.find((option) => option.value === value);
        if (selectedOption) {
            setSelectedOrder(selectedOption);
            navigate(`${location.pathname}?sort=${value}`);
        }
    };

    if (isLoading) {
        return <div>{t("Loading")}</div>;
    }

    if (isError) {
        return <div>Error: {error?.message}</div>;
    }

    return (
        <Container>
            <div className="mt-14 p-4">
                <h1 className="pb-5 text-3xl font-bold dark:text-stone-100">{t("Channels")}</h1>
                <div className="mb-4 flex items-center justify-end space-x-5">
                    <div className="space-x-2">
                        <DropdownButton
                            label={t("Sort")}
                            options={orderOptions}
                            onOptionSelected={handleOrderSelected}
                            icon="lucide:arrow-up-down"
                        />
                    </div>
                </div>
                <div className="grid grid-cols-1 gap-3 md:grid-cols-[repeat(auto-fit,minmax(220px,1fr))]">
                    {sortedChannels.map((channel) => {
                        const isLive =
                            !isLoadingStreams &&
                            streams?.some(
                                (stream) =>
                                    stream.broadcasterId === channel.broadcasterId && stream.type === "live"
                            );
                        return (
                            <Link
                                to={`${Pathnames.Video.Channel}/${channel.broadcasterLogin}`}
                                className={`bg-zinc-100 p-3 hover:bg-gray-100 dark:bg-custom_lightblue dark:hover:bg-custom_vista_blue ${
                                    isLive ? "relative" : ""
                                }`}
                                key={channel.broadcasterId}>
                                <div className="flex">
                                    <img
                                        className="h-10 w-10 rounded-full"
                                        src={channel.profilePicture}
                                        alt="Profile Picture"
                                    />
                                    <span className="flex items-center px-3 dark:text-white">
                                        {channel.broadcasterName}
                                    </span>
                                    {isLive && (
                                        <div className="align-center m-auto ml-0 h-4 w-4 rounded-full bg-red-500"></div>
                                    )}
                                </div>
                            </Link>
                        );
                    })}
                </div>
            </div>
        </Container>
    );
};
export default ChannelPage;
