FROM gcr.io/distroless/nodejs26-debian13:nonroot@sha256:10ec8cb93ef461563da50d4eb8dfac7d048783826825bf5b07510c2f34c14315
WORKDIR /app
COPY --chown=nonroot:nonroot server.mjs artifact.json csp.txt ./
COPY --chown=nonroot:nonroot site ./site
ENV PORT=8080
USER nonroot:nonroot
EXPOSE 8080
CMD ["/app/server.mjs"]
