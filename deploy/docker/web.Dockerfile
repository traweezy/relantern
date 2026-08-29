# syntax=docker/dockerfile:1.20
FROM node:26.8.1-bookworm-slim@sha256:367679cf9792759492a486e4aa4b421764d71a9546a6dae8aab81a99eb797b3e AS dependencies
ENV PNPM_HOME=/pnpm
ENV PATH=/pnpm:$PATH
RUN npm install --global pnpm@11.24.0
WORKDIR /workspace
COPY package.json pnpm-lock.yaml pnpm-workspace.yaml ./
COPY apps/web/package.json apps/web/package.json
COPY packages/api-client/package.json packages/api-client/package.json
COPY packages/design-tokens/package.json packages/design-tokens/package.json
COPY packages/domain/package.json packages/domain/package.json
RUN pnpm install --frozen-lockfile

FROM dependencies AS build
COPY . .
ENV NEXT_TELEMETRY_DISABLED=1
RUN pnpm --filter @relantern/web build

FROM dependencies AS development
COPY . .
ENV NEXT_TELEMETRY_DISABLED=1
CMD ["pnpm", "--filter", "@relantern/web", "dev"]

FROM gcr.io/distroless/nodejs26-debian13:nonroot@sha256:10ec8cb93ef461563da50d4eb8dfac7d048783826825bf5b07510c2f34c14315 AS production
WORKDIR /app
ENV HOSTNAME=0.0.0.0
ENV NODE_ENV=production
ENV NEXT_TELEMETRY_DISABLED=1
ENV PORT=3000
COPY --from=build --chown=nonroot:nonroot /workspace/apps/web/.next/standalone ./
COPY --from=build --chown=nonroot:nonroot /workspace/apps/web/.next/static ./apps/web/.next/static
COPY --from=build --chown=nonroot:nonroot /workspace/apps/web/public ./apps/web/public
USER nonroot:nonroot
EXPOSE 3000
CMD ["apps/web/server.js"]
