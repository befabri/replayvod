import { Routes, Route, useLocation, Navigate, Outlet } from "react-router-dom";
import { AuthProvider, useAuth } from "./pages/Auth";
import Navbar from "./components/Navbar";
import Sidebar from "./components/Sidebar";
import Vod from "./pages/Vod";
import Settings from "./pages/Settings.tsx";
import Follows from "./pages/Follows.tsx";
import AddChannel from "./pages/AddChannel.tsx";
import Dashboard from "./pages/Dashboard.tsx";
import Login from "./pages/Login.tsx";

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
          <Route path="/follows" element={<Follows />} />
          <Route path="/vod" element={<Vod />} />
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
  console.log(auth);
  if (!auth.user) {
    return <Navigate to="/login" state={{ from: location }} replace />;
  }

  return (
    <div>
      <Outlet />
    </div>
  );
}
