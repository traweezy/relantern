import type { Metadata } from "next";
import type { ReactNode } from "react";
import { memo } from "react";
import "./globals.css";

export const metadata: Metadata = {
  title: "Relantern",
  description: "Evidence-first personal developer intelligence.",
  robots: {
    follow: false,
    index: false,
  },
};

type RootLayoutProps = Readonly<{
  children: ReactNode;
}>;

const RootLayout = memo<RootLayoutProps>(({ children }) => (
  <html lang="en">
    <body>{children}</body>
  </html>
));

RootLayout.displayName = "RootLayout";

export default RootLayout;
