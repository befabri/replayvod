import { Routes, Route, useLocation, Navigate, Outlet } from "react-router-dom";
import { AuthProvider, useAuth } from "./context/Auth/Auth";
import Navbar from "./components/Layout/Navbar";
import Sidebar from "./components/Layout/Sidebar";
import Vod from "./pages/Vod/Vod";
import Settings from "./pages/Settings/Settings";
import Following from "./pages/Record/Following";
import AddChannel from "./pages/Record/AddChannel";
import Login from "./pages/Login/Login";
import ChannelPage from "./pages/Channel/Channel";
import Manage from "./pages/Record/Manage";
import HistoryPage from "./pages/Activity/History";
import Queue from "./pages/Activity/Queue";
import Tasks from "./pages/System/Tasks";
import Status from "./pages/System/Status";
import Logs from "./pages/System/Logs";
import Events from "./pages/System/Events";
import Watch from "./pages/Watch/Watch";
import VodCategory from "./pages/Vod/VodCategory";
import { DarkModeProvider } from "./context/Themes/DarkModeContext";

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
