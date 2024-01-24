import { useEffect, useState } from "react";
import { ManageSchedule } from "../../type";
import { Link } from "react-router-dom";
import { ApiRoutes, Pathnames, getApiRoute } from "../../type/routes";
import { useTranslation } from "react-i18next";
import HrefLink from "../../components/UI/Navigation/HrefLink";
import { qualityLabelToResolution } from "../../utils/utils";

const ScheduleStatistics: React.FC = () => {
    const { t } = useTranslation();
    const [schedule, setSchedule] = useState<ManageSchedule[]>([]);
    const [isLoading, setIsLoading] = useState(true);

    useEffect(() => {
        const fetchData = async () => {
            const url = getApiRoute(ApiRoutes.GET_SCHEDULE);
            const response = await fetch(url, {
                credentials: "include",
            });
            if (!response.ok) {
                throw new Error(`HTTP error! status: ${response.status}`);
            }
            const data = await response.json();
            const limitedData = data.slice(0, 3);
            setSchedule(limitedData || []);
            setIsLoading(false);
        };

        fetchData();
        const intervalId = setInterval(fetchData, 10000);

        return () => clearInterval(intervalId);
    }, []);

    return (
        <div className="rounded-lg bg-white p-4 shadow  dark:bg-custom_lightblue sm:p-5">
            <div className="mb-4 flex items-center justify-between">
                <h5 className="text-xl font-medium text-gray-500 dark:text-white">Téléchargements planifiés</h5>
                <HrefLink to={`${Pathnames.Schedule.Manage}`} style="normal">
                    Voir tout
                </HrefLink>
            </div>
            <div className="pt-4" id="about" role="tabpanel" aria-labelledby="about-tab">
                <ul role="list" className="divide-y divide-gray-200 dark:divide-slate-400">
                    {schedule.map((eventSub, idx) => (
                        <li className="py-3 sm:py-4" key={idx}>
                            <div className="flex items-center space-x-4">
                                <div className="flex-shrink-0">
                                    <Link
                                        to={`${
                                            Pathnames.Video.Channel
                                        }/${eventSub.channel.displayName.toLowerCase()}`}
                                        className="flex-shrink-0">
                                        <img
                                            className="h-10 w-10 rounded-full"
                                            src={eventSub.channel.profilePicture}
                                            alt="Profile Picture"
                                        />
                                    </Link>
                                </div>
                                <div className="min-w-0 flex-1">
                                    <p className="truncate font-medium text-gray-900 dark:text-white">
                                        <HrefLink
                                            to={`${
                                                Pathnames.Video.Channel
                                            }/${eventSub.channel.displayName.toLowerCase()}`}
                                            style="normal">
                                            {eventSub.channel.broadcasterName}
                                        </HrefLink>
                                    </p>
                                </div>
                                <div className="inline-flex items-center text-base font-semibold text-gray-900 dark:text-white">
                                    {qualityLabelToResolution(eventSub.quality)}p
                                </div>
                            </div>
                        </li>
                    ))}
                </ul>
            </div>
        </div>
    );
};

export default ScheduleStatistics;
