import type { Metadata } from "next";
import { connection } from "next/server";
import type { ReactNode } from "react";
import { memo } from "react";
import { getPublicBaseURL } from "@/server/public-config";
import "./globals.css";

export const metadata: Metadata = {
  title: "Relantern",
  description: "Evidence-first personal developer intelligence.",
  metadataBase: getPublicBaseURL(),
  robots: {
    follow: false,
    index: false,
  },
};

// Nonce-based CSP requires a blocking, fully request-rendered document.
export const instant = false;

type RootLayoutProps = Readonly<{
  children: ReactNode;
}>;

const RootLayoutContents = memo<RootLayoutProps>(({ children }) => (
  <html lang="en">
    <body>{children}</body>
  </html>
));

RootLayoutContents.displayName = "RootLayoutContents";

const RootLayout = async ({ children }: RootLayoutProps) => {
  // Per-request CSP nonces cannot be applied to a partial-prerendered shell.
  await connection();
  return <RootLayoutContents>{children}</RootLayoutContents>;
};

export default RootLayout;
