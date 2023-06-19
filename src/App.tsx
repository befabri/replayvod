import { Routes, Route, useLocation, Navigate, Outlet } from "react-router-dom";
import { AuthProvider, useAuth } from "./pages/Auth";
import Navbar from "./components/Navbar";
import Sidebar from "./components/Sidebar";
import Vod from "./pages/Vod";
import Settings from "./pages/Settings.tsx";
import Following from "./pages/Record/Following.tsx";
import AddChannel from "./pages/Record/AddChannel.tsx";
import Dashboard from "./pages/Dashboard.tsx";
import Login from "./pages/Login.tsx";
import Channel from "./pages/Channel.tsx";
import Manage from "./pages/Record/Manage.tsx";
import HistoryPage from "./pages/Activity/History.tsx";
import Queue from "./pages/Activity/Queue.tsx";

export default function App() {
  return (
    <AuthProvider>
      <AuthStatus />
      <Routes>
        <Route path="/login" element={<Login />} />
        <Route element={<RequireAuth />}>
          <Route path="/" element={<Dashboard />} />
          <Route path="/dashboard" element={<Dashboard />} />
          <Route path="/settings" element={<Settings />} />
          <Route path="/schedule/add" element={<AddChannel />} />
          <Route path="/schedule/manage" element={<Manage />} />
          <Route path="/schedule/following" element={<Following />} />
          <Route path="/activity/queue" element={<Queue />} />
          <Route path="/activity/history" element={<HistoryPage />} />
          <Route path="/vod" element={<Vod />} />
          <Route path="/channel/:id" element={<Channel />} />
        </Route>
      </Routes>
    </AuthProvider>
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
    <div>
      <Outlet />
    </div>
  );
}
