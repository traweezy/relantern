"use client";

import { memo, useEffect, useState } from "react";

const ReadingProgressComponent = () => {
  const [progress, setProgress] = useState(0);

  useEffect(() => {
    const updateProgress = () => {
      const available = document.documentElement.scrollHeight - window.innerHeight;
      const nextProgress = available <= 0 ? 100 : Math.min(100, (window.scrollY / available) * 100);
      setProgress(nextProgress);
    };
    updateProgress();
    window.addEventListener("scroll", updateProgress, { passive: true });
    window.addEventListener("resize", updateProgress);
    return () => {
      window.removeEventListener("scroll", updateProgress);
      window.removeEventListener("resize", updateProgress);
    };
  }, []);

  return (
    <div aria-hidden="true" className="reading-progress">
      <progress max={100} value={progress} />
    </div>
  );
};

export const ReadingProgress = memo(ReadingProgressComponent);
ReadingProgress.displayName = "ReadingProgress";
