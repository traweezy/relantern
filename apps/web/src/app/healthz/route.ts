export const GET = (): Response =>
  Response.json(
    {
      service: "web",
      status: "ok",
    },
    {
      headers: {
        "Cache-Control": "no-store",
      },
    },
  );
