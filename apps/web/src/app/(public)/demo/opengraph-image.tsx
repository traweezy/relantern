import { ImageResponse } from "next/og";

export const alt = "Relantern evidence-first developer intelligence demonstration";
export const contentType = "image/png";
export const size = { height: 630, width: 1200 };

const OpenGraphImage = () =>
  new ImageResponse(
    <div
      style={{
        alignItems: "stretch",
        background: "#111a27",
        color: "#edf4f7",
        display: "flex",
        flexDirection: "column",
        fontFamily: "ui-sans-serif, system-ui, sans-serif",
        height: "100%",
        justifyContent: "space-between",
        padding: "76px 84px",
        width: "100%",
      }}
    >
      <div style={{ alignItems: "center", display: "flex", gap: 22 }}>
        <div
          style={{
            alignItems: "center",
            border: "2px solid #55ddb2",
            borderRadius: 20,
            color: "#55ddb2",
            display: "flex",
            fontSize: 34,
            fontWeight: 700,
            height: 76,
            justifyContent: "center",
            width: 76,
          }}
        >
          R
        </div>
        <div style={{ display: "flex", flexDirection: "column" }}>
          <span style={{ fontSize: 34, fontWeight: 700 }}>Relantern</span>
          <span style={{ color: "#9fb0bf", fontSize: 20 }}>Private developer intelligence</span>
        </div>
      </div>
      <div style={{ display: "flex", flexDirection: "column", maxWidth: 980 }}>
        <span style={{ color: "#55ddb2", fontSize: 19, letterSpacing: 4 }}>
          EVIDENCE-FIRST · OWNER-OPERATED
        </span>
        <span
          style={{
            fontSize: 70,
            fontWeight: 620,
            letterSpacing: -4,
            lineHeight: 1.05,
            marginTop: 24,
          }}
        >
          Know what changed. Keep the proof attached.
        </span>
      </div>
      <div
        style={{
          borderTop: "1px solid #334455",
          color: "#9fb0bf",
          display: "flex",
          fontSize: 20,
          justifyContent: "space-between",
          paddingTop: 24,
        }}
      >
        <span>Today · Live · Claim → evidence</span>
        <span>Illustrative fixture demonstration</span>
      </div>
    </div>,
    size,
  );

export default OpenGraphImage;
