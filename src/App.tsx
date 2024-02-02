import { Routes, Route, useLocation, Navigate, Outlet } from "react-router-dom";
import { ReactQueryDevtools } from "@tanstack/react-query-devtools";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { AuthProvider, useAuth } from "./context/Auth/Auth";
import Navbar from "./components/Layout/Navbar";
import Sidebar from "./components/Layout/Sidebar";
import { DarkModeProvider } from "./context/Themes/DarkModeContext";
import { Pathnames } from "./type/routes";
import VideosPage from "./pages/Videos/Index";
import SettingsPage from "./pages/Settings/Index";
import AddChannelPage from "./pages/Record/AddChannelPage";
import ManagePage from "./pages/Record/ManagePage";
import HistoryPage from "./pages/Activity/HistoryPage";
import CategoryPage from "./pages/Videos/Category/Index";
import CategoryDetailPage from "./pages/Videos/Category/CategoryDetailPage";
import ChannelDetailPage from "./pages/Videos/Channel/ChannelDetailPage";
import ChannelPage from "./pages/Videos/Channel/Index";
import TasksPage from "./pages/System/TasksPage";
import EventsPage from "./pages/System/EventsPage";
import LogsPage from "./pages/System/LogsPage";
import WatchPage from "./pages/Watch/Index";
import LoginPage from "./pages/Login/Index";
import QueuePage from "./pages/Activity/QueuePage";
import DashboardPage from "./pages/Dashboard/Index";
import EventSubPage from "./pages/System/EventSub";
import NotFoundPage from "./pages/NotFoundPage";

const queryClient = new QueryClient();

export default function App() {
    return (
        <QueryClientProvider client={queryClient}>
            <DarkModeProvider>
                <AuthProvider>
                    <AuthStatus />
                    <Routes>
                        <Route path={Pathnames.Login} element={<LoginPage />} />
                        <Route element={<RequireAuth />}>
                            <Route path={Pathnames.Home} element={<DashboardPage />} />
                            <Route path={Pathnames.Settings} element={<SettingsPage />} />
                            <Route path={Pathnames.Schedule.Add} element={<AddChannelPage />} />
                            <Route path={Pathnames.Schedule.Manage} element={<ManagePage />} />
                            <Route path={Pathnames.Activity.Queue} element={<QueuePage />} />
                            <Route path={Pathnames.Activity.History} element={<HistoryPage />} />
                            <Route path={Pathnames.Video.Video} element={<VideosPage />} />
                            <Route path={Pathnames.Video.Category} element={<CategoryPage />} />
                            <Route path={Pathnames.Video.CategoryDetail} element={<CategoryDetailPage />} />
                            <Route path={Pathnames.Video.Channel} element={<ChannelPage />} />
                            <Route path={Pathnames.Video.ChannelDetail} element={<ChannelDetailPage />} />
                            <Route path={Pathnames.System.EventSub} element={<EventSubPage />} />
                            <Route path={Pathnames.System.Tasks} element={<TasksPage />} />
                            <Route path={Pathnames.System.Events} element={<EventsPage />} />
                            <Route path={Pathnames.System.Logs} element={<LogsPage />} />
                            <Route path={Pathnames.WatchDetail} element={<WatchPage />} />
                            <Route path="*" element={<NotFoundPage />} />
                        </Route>
                    </Routes>
                </AuthProvider>
            </DarkModeProvider>
        </QueryClientProvider>
    );
}

function AuthStatus() {
    const auth = useAuth();

    if (!auth.user) {
        return null;
    }

    return (
        <>
            {auth.user && (
                <div>
                    <Navbar />
                    <Sidebar isOpenSideBar={false} onCloseSidebar={handleSidebarClose} />
                    <ReactQueryDevtools initialIsOpen />
                </div>
            )}
        </>
    );
}

function handleSidebarClose(): void {
    throw new Error("Function not implemented.");
}

function RequireAuth() {
    const auth = useAuth();
    const location = useLocation();

    if (auth.isLoading) {
        return (
            <div>
                <Navbar />
                <Sidebar isOpenSideBar={false} onCloseSidebar={handleSidebarClose} />
            </div>
        );
    }
    if (!auth.user) {
        return <Navigate to="/login" state={{ from: location }} replace />;
    }

    return (
        <main className="md:ml-56">
            <Outlet />
        </main>
    );
}
