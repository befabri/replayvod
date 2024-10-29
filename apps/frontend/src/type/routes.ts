const ROOT_URL = import.meta.env.VITE_ROOTURL;

export const Pathnames = {
    Home: "/" as const,
    Login: "/login" as const,
    Settings: "/settings" as const,
    Schedule: {
        Add: "/schedule/add" as const,
        Manage: "/schedule/manage" as const,
    },
    Activity: {
        Queue: "/activity/queue" as const,
        History: "/activity/history" as const,
    },
    Video: {
        Video: "/videos" as const,
        Category: "/videos/category" as const,
        CategoryDetail: "/videos/category/:id" as const,
        Channel: "/videos/channel" as const,
        ChannelDetail: "/videos/channel/:id" as const,
    },
    System: {
        EventSub: "/system/event-sub" as const,
        Status: "/system/status" as const,
        Tasks: "/system/tasks" as const,
        Events: "/system/events" as const,
        Logs: "/system/logs" as const,
    },
    Watch: "/watch/" as const,
    WatchDetail: "/watch/:id" as const,
};

export const getApiRoute = (route: string, placeholder?: string, value?: string | number) => {
    const updatedRoute =
        placeholder && value !== undefined ? route.replace(`:${placeholder}`, String(value)) : route;
    return `${ROOT_URL}${updatedRoute}`;
};

export enum ApiRoutes {
    GET_AUTH_TWITCH = "/api/auth/twitch",
    GET_AUTH_CALLBACK = "/api/auth/twitch/callback",
    GET_AUTH_CHECK_SESSION = "/api/auth/check-session",
    GET_AUTH_USER = "/api/auth/user",
    GET_AUTH_REFRESH = "/api/auth/refresh",
    POST_AUTH_SIGNOUT = "/api/auth/signout",
    GET_USER_FOLLOWED_STREAMS = "/api/user/followed-streams",
    GET_USER_FOLLOWED_CHANNELS = "/api/user/followed-channels",
    GET_CHANNEL_ID = "/api/channel/:id",
    PUT_CHANNEL_ID = "/api/channel/:id",
    GET_CHANNEL = "/api/channel",
    GET_CHANNEL_NAME_NAME = "/api/channel/name/:name",
    GET_CHANNEL_LAST_LIVE = "/api/channel/stream/lastlive",
    GET_DOWNLOAD_USER_ID = "/api/download/user/:id",
    GET_DOWNLOAD_STREAM_ID = "/api/download/stream/:id",
    POST_SCHEDULE = "/api/schedule",
    PUT_SCHEDULE_EDIT = "/api/schedule/:id",
    DELETE_SCHEDULE = "/api/schedule/:id",
    GET_SCHEDULE = "/api/schedule",
    POST_TOGGLE_SCHEDULE = "/api/schedule/:id/toggle",
    GET_DOWNLOAD_STATUS_ID = "/api/download/status/:id",
    GET_VIDEO_PLAY_ID = "/api/video/play/:id",
    GET_VIDEO_ID = "/api/video/:id",
    GET_VIDEO_ALL = "/api/video/all",
    GET_VIDEO_FINISHED = "/api/video/finished",
    GET_VIDEO_PENDING = "/api/video/pending",
    GET_VIDEO_BY_CATEGORY = "/api/video/category/:name",
    GET_CATEGORY_ALL = "/api/category/",
    GET_CATEGORY = "/api/category/detail/:name",
    GET_VIDEO_CATEGORY_ALL = "/api/category/videos",
    GET_VIDEO_CATEGORY_ALL_DONE = "/api/category/videos/done",
    GET_VIDEO_CHANNEL_BROADCASTERLOGIN = "/api/video/channel/:broadcasterLogin",
    GET_VIDEO_UPDATE_MISSING = "/api/video/update/missing",
    GET_VIDEO_THUMBNAIL_ID = "/api/video/thumbnail/:id",
    GET_VIDEO_STATISTICS = "/api/video/statistics",
    GET_TASK = "/api/task/",
    GET_TASK_ID = "/api/task/:id",
    GET_TASK_RUN_ID = "/api/task/run/:id",
    GET_LOG_FILES_ID = "/api/log/files/:id",
    GET_LOG_FILES = "/api/log/files",
    GET_LOG_DOMAINS_ID = "/api/log/domains/:id",
    GET_LOG_DOMAINS = "/api/log/domains",
    GET_SETTINGS = "/api/settings/",
    POST_SETTINGS = "/api/settings/",
    GET_EVENT_SUB_SUBSCRIPTIONS = "/api/event-sub/subscriptions",
    GET_EVENT_SUB_COSTS = "/api/event-sub/costs",
}
