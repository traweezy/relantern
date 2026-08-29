# syntax=docker/dockerfile:1.20
FROM golang:1.27.0-bookworm@sha256:ded31c68586d2e49e760acc2e65a884b23d032e9bbbed0ae0c55abd3fcaf4452 AS build
WORKDIR /workspace
COPY go.mod go.sum ./
RUN go mod download
COPY . .
ARG TARGET
RUN test "${TARGET}" = "fake-source" -o "${TARGET}" = "fake-openai" -o "${TARGET}" = "fake-delivery" \
    && CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/provider "./cmd/${TARGET}"

FROM gcr.io/distroless/static-debian13:nonroot@sha256:1c2c046bc09ed40fad370b599a0b1ae7987f55b01e247cf27a7c27cd97e5bbc7 AS production
WORKDIR /app
COPY --from=build --chown=nonroot:nonroot /out/provider /app/provider
USER nonroot:nonroot
ENTRYPOINT ["/app/provider"]
