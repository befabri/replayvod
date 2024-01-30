import { Schedule } from "../../type";
import { Link } from "react-router-dom";
import { ApiRoutes, Pathnames } from "../../type/routes";
import { useTranslation } from "react-i18next";
import HrefLink from "../../components/UI/Navigation/HrefLink";
import { customFetch } from "../../utils/utils";
import { useQuery } from "@tanstack/react-query";

const ScheduleStatistics: React.FC = () => {
    const { t } = useTranslation();

    const {
        data: schedules,
        isLoading,
        isError,
        error,
    } = useQuery<Schedule[], Error>({
        queryKey: ["schedules"],
        queryFn: (): Promise<Schedule[]> => customFetch(ApiRoutes.GET_SCHEDULE),
        staleTime: 5 * 60 * 1000,
    });

    if (isLoading) {
        return <div>{t("Loading")}</div>;
    }

    if (isError || !schedules) {
        return <div>Error: {error?.message}</div>;
    }

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
                    {schedules.slice(0, 4).map((schedule, idx) => (
                        <li className="py-3 sm:py-4" key={idx}>
                            <div className="flex items-center space-x-4">
                                <div className="flex-shrink-0">
                                    <Link
                                        to={`${
                                            Pathnames.Video.Channel
                                        }/${schedule.channel.displayName.toLowerCase()}`}
                                        className="flex-shrink-0">
                                        <img
                                            className="h-10 w-10 rounded-full"
                                            src={schedule.channel.profilePicture}
                                            alt="Profile Picture"
                                        />
                                    </Link>
                                </div>
                                <div className="min-w-0 flex-1">
                                    <p className="truncate font-medium text-gray-900 dark:text-white">
                                        <HrefLink
                                            to={`${
                                                Pathnames.Video.Channel
                                            }/${schedule.channel.displayName.toLowerCase()}`}
                                            style="normal">
                                            {schedule.channel.broadcasterName}
                                        </HrefLink>
                                    </p>
                                </div>
                                <div className="inline-flex items-center text-base font-semibold text-gray-900 dark:text-white">
                                    {schedule.quality}p
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
