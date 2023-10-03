import React, { useState } from 'react';

const AuthContext = React.createContext({
  isLoggedIn: false,
  setLoginStatus: (_isLoggedIn: boolean) => {}
});

export function AuthProvider({ children }: { children: React.ReactNode }) {
  const [isLoggedIn, setIsLoggedIn] = useState(false);

  const setLoginStatus = (value: boolean) => {
    setIsLoggedIn(value);
  };

  return (
    <AuthContext.Provider value={{isLoggedIn, setLoginStatus }}>
      {children}
    </AuthContext.Provider>
  );
}

export default AuthContext;
