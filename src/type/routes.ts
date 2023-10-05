const ROOT_URL = import.meta.env.VITE_ROOTURL;

export const Pathnames = {
    Home: "/" as const,
    Settings: "/settings" as const,
    Schedule: {
        Add: "/schedule/add" as const,
        Manage: "/schedule/manage" as const,
        Following: "/schedule/following" as const,
    },
    Activity: {
        Queue: "/activity/queue" as const,
        History: "/activity/history" as const,
    },
    Vod: "/vod" as const,
    Channel: "/channel/" as const,
    System: {
        Status: "/system/status" as const,
        Tasks: "/system/tasks" as const,
        Events: "/system/events" as const,
        Logs: "/system/logs" as const,
    },
    Watch: "/watch/" as const,
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
    GET_USER_FOLLOWED_STREAMS = "/api/user/followedstreams",
    GET_USER_FOLLOWED_CHANNELS = "/api/user/followedchannels",
    GET_CATEGORY = "/api/category/",
    GET_CHANNEL_ID = "/api/channel/:id",
    PUT_CHANNEL_ID = "/api/channel/:id",
    GET_CHANNEL = "/api/channel/",
    POST_CHANNEL = "/api/channel/",
    GET_CHANNEL_NAME_NAME = "/api/channel/name/:name",
    GET_DOWNLOAD_USER_ID = "/api/download/user/:id",
    GET_DOWNLOAD_STREAM_ID = "/api/download/stream/:id",
    POST_DOWNLOAD_SCHEDULE = "/api/download/schedule",
    GET_DOWNLOAD_STATUS_ID = "/api/download/status/:id",
    GET_VIDEO_PLAY_ID = "/api/video/play/:id",
    GET_VIDEO_ID = "/api/video/:id",
    GET_VIDEO_ALL = "/api/video/all",
    GET_VIDEO_FINISHED = "/api/video/finished",
    GET_VIDEO_USER_ID = "/api/video/user/:id",
    GET_VIDEO_UPDATE_MISSING = "/api/video/update/missing",
    GET_VIDEO_THUMBNAIL_ID = "/api/video/thumbnail/:id",
    GET_TWITCH_UPDATE_GAMES = "/api/twitch/update/games",
    GET_TWITCH_EVENTSUB_SUBSCRIPTIONS = "/api/twitch/eventsub/subscriptions",
    GET_TWITCH_EVENTSUB_COSTS = "/api/twitch/eventsub/costs",
    GET_TASK = "/api/task/",
    GET_TASK_ID = "/api/task/:id",
    GET_TASK_RUN_ID = "/api/task/run/:id",
    GET_LOG_FILES_ID = "/api/log/files/:id",
    GET_LOG_FILES = "/api/log/files",
    GET_LOG_DOMAINS_ID = "/api/log/domains/:id",
    GET_LOG_DOMAINS = "/api/log/domains",
    POST_WEBHOOK_WEBHOOKS = "/api/webhook/webhooks",
    DELETE_WEBHOOK_WEBHOOKS = "/api/webhook/webhooks",
    POST_WEBHOOK_WEBHOOKS_CALLBACK = "/api/webhook/webhooks/callback",
    GET_SETTINGS = "/api/settings/",
    POST_SETTINGS = "/api/settings/",
}
