import { Routes, Route, useLocation, Navigate, Outlet } from "react-router-dom";
import { AuthProvider, useAuth } from "./pages/Auth";
import Navbar from "./components/Navbar";
import Sidebar from "./components/Sidebar";
import Vod from "./pages/Vod";
import Settings from "./pages/Settings.tsx";
import Following from "./pages/Following.tsx";
import AddChannel from "./pages/AddChannel.tsx";
import Dashboard from "./pages/Dashboard.tsx";
import Login from "./pages/Login.tsx";
import Channel from "./pages/Channel.tsx";
import Manage from "./pages/Manage.tsx";

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
          <Route path="/add" element={<AddChannel />} />
          <Route path="/following" element={<Following />} />
          <Route path="/vod" element={<Vod />} />
          <Route path="/channel/:id" element={<Channel />} />
          <Route path="/manage" element={<Manage />} />
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
          <Sidebar />
        </div>
      )}
    </>
  );
}

function RequireAuth() {
  let auth = useAuth();
  let location = useLocation();

  if (auth.isLoading) {
    return (
      <div>
        <Navbar />
        <Sidebar />
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
