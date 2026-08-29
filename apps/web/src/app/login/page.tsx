import type { Metadata } from "next";
import { redirect } from "next/navigation";
import { memo, Suspense } from "react";
import { SignInButton } from "@/components/auth/sign-in-button";
import { getOwnerSession } from "@/server/auth/session";

export const metadata: Metadata = {
  title: "Sign in | Relantern",
};

const LoginCard = async () => {
  if ((await getOwnerSession()) !== null) {
    redirect("/");
  }
  return (
    <section aria-labelledby="login-title" className="login-card">
      <div aria-hidden="true" className="login-lantern">
        <span>R</span>
      </div>
      <p className="eyebrow">Private developer intelligence</p>
      <h1 id="login-title">Your signal, without the noise.</h1>
      <p className="login-lede">
        Relantern turns trusted technical evidence into a focused daily view. Access is limited to
        the configured owner account.
      </p>
      <SignInButton />
      <div className="login-assurance">
        <span aria-hidden="true" className="security-dot" />
        <p>GitHub verifies identity. Relantern verifies the numeric owner ID.</p>
      </div>
      <p className="login-demo-link">
        Reviewing the product? <a href="/demo">Open the isolated demo</a>.
      </p>
    </section>
  );
};

const LoginSkeleton = memo(() => (
  <section aria-busy="true" aria-label="Checking session" className="login-card login-card-loading">
    <div className="skeleton-line skeleton-line-short" />
    <div className="skeleton-line" />
    <div className="skeleton-panel skeleton-panel-compact" />
    <span className="sr-only">Checking session</span>
  </section>
));

LoginSkeleton.displayName = "LoginSkeleton";

const LoginPage = memo(() => (
  <main className="login-shell">
    <div aria-hidden="true" className="login-grid" />
    <Suspense fallback={<LoginSkeleton />}>
      <LoginCard />
    </Suspense>
    <p className="login-footnote">Evidence-first. Owner-operated. Private by default.</p>
  </main>
));

LoginPage.displayName = "LoginPage";

export default LoginPage;
