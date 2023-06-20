import * as React from "react";

interface AuthContextType {
  user: any;
  signin: () => void;
  signout: () => void;
  isLoading: boolean;
  isAuthenticated: boolean;
}

interface AuthProviderProps {
  children: React.ReactNode;
}

const AuthContext = React.createContext<AuthContextType | undefined>(undefined);

const AuthProvider: React.FC<AuthProviderProps> = ({ children }) => {
  const [user, setUser] = React.useState<any>(null);
  const [isLoading, setIsLoading] = React.useState<boolean>(true);
  const [isAuthenticated, setIsAuthenticated] = React.useState<boolean>(false);

  React.useEffect(() => {
    checkSession();
    refreshTokenCycle();
  }, []);

  async function checkSession() {
    setIsLoading(true);
    try {
      if (!isAuthenticated) {
        const response = await fetch("http://localhost:3000/api/auth/check-session", { credentials: "include" });
        const data = await response.json();
        if (data.status === "authenticated") {
          setUser(data.token);
          setIsAuthenticated(true);
        } else {
          setUser(null);
          setIsAuthenticated(false);
        }
      }
    } catch (error) {
      console.log("Check session failed", error);
    } finally {
      setIsLoading(false);
    }
  }

  async function refreshToken() {
    try {
      const response = await fetch("http://localhost:3000/api/auth/refresh", { credentials: "include" });
      const data = await response.json();
      setUser(data.token);
      console.log("Token refreshed");
    } catch (error) {
      console.error("Failed to refresh token", error);
    }
  }

  function refreshTokenCycle() {
    const refreshInterval = setInterval(refreshToken, 1000 * 60 * 60);
    return () => clearInterval(refreshInterval);
  }

  const signin = () => {
    window.location.href = "http://localhost:3000/api/auth/twitch";
  };

  const signout = () => {
    setUser(null);
    setIsAuthenticated(false);
  };

  return (
    <AuthContext.Provider value={{ user, signin, signout, isLoading, isAuthenticated }}>
      {children}
    </AuthContext.Provider>
  );
};

const useAuth = () => {
  const context = React.useContext(AuthContext);
  if (context === undefined) {
    throw new Error("useAuth must be used within an AuthProvider");
  }
  return context;
};

export { AuthProvider, useAuth };
