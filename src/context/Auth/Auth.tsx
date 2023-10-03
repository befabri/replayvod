import * as React from "react";
import { ApiRoutes, getApiRoute } from "../../type/routes";

interface AuthContextType {
    user: any;
    signIn: () => void;
    signOut: () => void;
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
    const ROOT_URL = import.meta.env.VITE_ROOTURL;

    React.useEffect(() => {
        checkSession();
        refreshTokenCycle();
    }, []);

    async function checkSession() {
        setIsLoading(true);
        try {
            let url = getApiRoute(ApiRoutes.GET_AUTH_CHECK_SESSION);
            const response = await fetch(url, {
                credentials: "include",
            });

            if (response.status === 200) {
                const data = await response.json();
                setUser(data.user);
                setIsAuthenticated(true);
            } else {
                setUser(null);
                setIsAuthenticated(false);
            }
        } catch (error) {
            console.log("Check session failed", error);
        } finally {
            setIsLoading(false);
        }
    }

    async function refreshToken() {
        try {
            let url = getApiRoute(ApiRoutes.GET_AUTH_REFRESH);
            const response = await fetch(url, {
                credentials: "include",
            });
            const data = await response.json();
            if (data.status === "authenticated") {
                console.log("Token refreshed");
                setUser(data.user);
                setIsAuthenticated(true);
            } else {
                setUser(null);
                setIsAuthenticated(false);
            }
        } catch (error) {
            console.error("Failed to refresh token", error);
        }
    }

    function refreshTokenCycle() {
        const refreshInterval = setInterval(refreshToken, 1000 * 60 * 60);
        return () => clearInterval(refreshInterval);
    }

    const signIn = () => {
        window.location.href = `${ROOT_URL}/api/auth/twitch`;
    };

    const signOut = async () => {
        try {
            let url = getApiRoute(ApiRoutes.POST_AUTH_SIGNOUT);
            await fetch(url, {
                method: "POST",
                credentials: "include",
            });
        } catch (error) {
            console.error("Failed to sign out", error);
        }
        setUser(null);
        setIsAuthenticated(false);
    };

    return (
        <AuthContext.Provider value={{ user, signIn, signOut, isLoading, isAuthenticated }}>
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
