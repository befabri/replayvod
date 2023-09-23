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
