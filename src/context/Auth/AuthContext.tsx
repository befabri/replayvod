import React, { useState } from "react";

const AuthContext = React.createContext({
    isLoggedIn: false,
    // eslint-disable-next-line @typescript-eslint/no-unused-vars
    setLoginStatus: (_isLoggedIn: boolean) => {
        /* intentionally left blank */
    },
});

export function AuthProvider({ children }: { children: React.ReactNode }) {
    const [isLoggedIn, setIsLoggedIn] = useState(false);

    const setLoginStatus = (value: boolean) => {
        setIsLoggedIn(value);
    };

    return <AuthContext.Provider value={{ isLoggedIn, setLoginStatus }}>{children}</AuthContext.Provider>;
}

export default AuthContext;
