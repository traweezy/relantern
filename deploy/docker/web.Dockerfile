# syntax=docker/dockerfile:1.20
FROM node:24.20.0-bookworm-slim@sha256:ba849c60be29959425b8734d57b8b4b7d56f98edd9504c9af091d5281095a71e AS dependencies
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

FROM gcr.io/distroless/nodejs24-debian13:nonroot@sha256:774b7d020b24214835769e24c3544835526cd0288f0b094eae48e8b2c2429a79 AS production
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
