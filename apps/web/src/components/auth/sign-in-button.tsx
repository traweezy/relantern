"use client";

import { memo, useCallback, useState } from "react";
import { authClient } from "@/lib/auth-client";

const SignInButtonComponent = () => {
  const [error, setError] = useState<string | null>(null);
  const [pending, setPending] = useState(false);

  const handleSignIn = useCallback(async () => {
    setError(null);
    setPending(true);
    const result = await authClient.signIn.social({
      callbackURL: "/",
      errorCallbackURL: "/login",
      provider: "github",
    });
    if (result.error !== null) {
      setError("Sign-in could not be completed. Confirm the configured owner account and retry.");
      setPending(false);
    }
  }, []);

  return (
    <div className="auth-action">
      <button className="primary-button" disabled={pending} onClick={handleSignIn} type="button">
        {pending ? "Opening GitHub…" : "Continue with GitHub"}
      </button>
      <p aria-live="polite" className="auth-status" role="status">
        {error}
      </p>
    </div>
  );
};

export const SignInButton = memo(SignInButtonComponent);
SignInButton.displayName = "SignInButton";
