import { Routes, Route, useLocation, Navigate, Outlet } from "react-router-dom";
import { AuthProvider, useAuth } from "./pages/Auth";
import Navbar from "./components/Navbar";
import Sidebar from "./components/Sidebar";
import Vod from "./pages/Vod";
import Settings from "./pages/Settings.tsx";
import Following from "./pages/Record/Following.tsx";
import AddChannel from "./pages/Record/AddChannel.tsx";
import Login from "./pages/Login.tsx";
import ChannelPage from "./pages/ChannelPage.tsx";
import Manage from "./pages/Record/Manage.tsx";
import HistoryPage from "./pages/Activity/History.tsx";
import Queue from "./pages/Activity/Queue.tsx";
import Tasks from "./pages/System/Tasks.tsx";
import Status from "./pages/System/Status.tsx";
import Logs from "./pages/System/Logs.tsx";
import Events from "./pages/System/Events.tsx";
import Watch from "./pages/Watch.tsx";
import VodCategory from "./pages/VodCategory.tsx";
import { DarkModeProvider } from "./context/DarkModeContext.tsx";

export default function App() {
    return (
        <DarkModeProvider>
            <AuthProvider>
                <AuthStatus />
                <Routes>
                    <Route path="/login" element={<Login />} />
                    <Route element={<RequireAuth />}>
                        <Route path="/" element={<Vod />} />
                        <Route path="/settings" element={<Settings />} />
                        <Route path="/schedule/add" element={<AddChannel />} />
                        <Route path="/schedule/manage" element={<Manage />} />
                        <Route path="/schedule/following" element={<Following />} />
                        <Route path="/activity/queue" element={<Queue />} />
                        <Route path="/activity/history" element={<HistoryPage />} />
                        <Route path="/vod" element={<Vod />} />
                        <Route path="/vod/:id" element={<VodCategory />} />
                        <Route path="/channel/:id" element={<ChannelPage />} />
                        <Route path="/system/status" element={<Status />} />
                        <Route path="/system/tasks" element={<Tasks />} />
                        <Route path="/system/events" element={<Events />} />
                        <Route path="/system/logs" element={<Logs />} />
                        <Route path="/watch/:id" element={<Watch />} />
                    </Route>
                </Routes>
            </AuthProvider>
        </DarkModeProvider>
    );
}

function AuthStatus() {
    let auth = useAuth();

    if (!auth.user) {
        return null;
    }

    return (
        <>
            {auth.user && (
                <div>
                    <Navbar />
                    <Sidebar isOpenSideBar={false} onCloseSidebar={handleSidebarClose} />
                </div>
            )}
        </>
    );
}

function handleSidebarClose(): void {
    throw new Error("Function not implemented.");
}

function RequireAuth() {
    let auth = useAuth();
    let location = useLocation();

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
