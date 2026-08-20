import { createContext, useContext, type ReactNode } from "react";
import { useQuery } from "@tanstack/react-query";
import { api, type User } from "./api";

interface AuthValue {
  user: User | null;
  loading: boolean;
  login: () => void;
  loginTo: (returnTo: string) => void;
  logout: () => Promise<void>;
  isStaff: boolean;
  isAdmin: boolean;
}

const AuthContext = createContext<AuthValue>({
  user: null,
  loading: true,
  login: () => {},
  loginTo: () => {},
  logout: async () => {},
  isStaff: false,
  isAdmin: false,
});

export function AuthProvider({ children }: { children: ReactNode }) {
  const { data, isLoading } = useQuery({
    queryKey: ["me"],
    queryFn: api.me,
    staleTime: 60_000,
  });
  const user = data ?? null;

  const value: AuthValue = {
    user,
    loading: isLoading,
    login: () => {
      window.location.href = api.loginUrl();
    },
    loginTo: (returnTo: string) => {
      window.location.href = api.loginUrl(returnTo);
    },
    logout: async () => {
      const res = await api.logout().catch(() => ({ logoutUrl: undefined }));
      // Leave via a full-page navigation, not a cache invalidation: C2's logout
      // endpoint has to end the SSO session too, and re-rendering as anonymous
      // first would let RequireAuth bounce us straight back into a silent login.
      window.location.assign(res.logoutUrl || api.homeUrl());
    },
    isStaff: user?.role === "staff" || user?.role === "admin",
    isAdmin: user?.role === "admin",
  };
  return <AuthContext.Provider value={value}>{children}</AuthContext.Provider>;
}

export function useAuth() {
  return useContext(AuthContext);
}
