import * as React from "react";
import { Routes, Route, Link, useLocation, Navigate, Outlet } from "react-router-dom";
import { AuthProvider, useAuth } from "./pages/Auth";
import Navbar from "./components/Navbar";
import Sidebar from "./components/Sidebar";
import Vod from "./pages/Vod";
import Settings from "./pages/Settings.tsx";
import Follows from "./pages/Follows.tsx";
import AddChannel from "./pages/AddChannel.tsx";
import Dashboard from "./pages/Dashboard.tsx";

export default function App() {
  return (
    <AuthProvider>
      <AuthStatus />
      <Routes>
        <Route path="/" element={<PublicPage />} />
        <Route path="/login" element={<LoginPage />} />
        <Route element={<RequireAuth />}>
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

// function Layout() {
//   return (
//     <div>
//       <AuthStatus />
//       <ul>
//         <li>
//           <Link to="/">Public Page</Link>
//         </li>
//         <li>
//           <Link to="/vod">Protected Page</Link>
//         </li>
//       </ul>

//       <Outlet />
//     </div>
//   );
// }

function AuthStatus() {
  let auth = useAuth();
  if (auth.isLoading) {
    return         <div>
    <Navbar />
    <Sidebar />
  </div>
  }
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
    return         <div>
    <Navbar />
    <Sidebar />
  </div>
  }
  console.log(auth)
  if (!auth.user) {
    return <Navigate to="/login" state={{ from: location }} replace />;
  }

  return (
    <div>
      <Outlet /> 
    </div>
  );
}

function LoginPage() {
  // let navigate = useNavigate();
  let location = useLocation();
  let auth = useAuth();

  let from = location.state?.from?.pathname || "/";

  function handleSubmit(event: React.FormEvent<HTMLFormElement>) {
    event.preventDefault();

    // let formData = new FormData(event.currentTarget);
    // let username = formData.get("username") as string;

    auth.signin();
  }

  return (
    <div>
      <p>You must log in to view the page at {from}</p>

      <form onSubmit={handleSubmit}>
        <label>
          Username: <input name="username" type="text" />
        </label>{" "}
        <button type="submit">Login</button>
      </form>
    </div>
  );
}

function PublicPage() {
  return <h3>Public</h3>;
}