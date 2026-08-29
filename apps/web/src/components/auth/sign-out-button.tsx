"use client";

import { memo, useCallback, useState } from "react";
import { authClient } from "@/lib/auth-client";

const SignOutButtonComponent = () => {
  const [error, setError] = useState<string | null>(null);
  const [pending, setPending] = useState(false);
  const handleSignOut = useCallback(async () => {
    setError(null);
    setPending(true);
    const result = await authClient.signOut();
    if (result.error !== null) {
      setError("Sign out failed. Retry.");
      setPending(false);
      return;
    }
    window.location.assign("/login");
  }, []);

  return (
    <>
      <button className="quiet-button" disabled={pending} onClick={handleSignOut} type="button">
        {pending ? "Signing out…" : "Sign out"}
      </button>
      <span aria-live="polite" className="sidebar-error" role="status">
        {error}
      </span>
    </>
  );
};

export const SignOutButton = memo(SignOutButtonComponent);
SignOutButton.displayName = "SignOutButton";
