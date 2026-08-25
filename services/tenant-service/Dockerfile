FROM golang:1.26.6-alpine@sha256:3889b425f035be855a72fb4755265311293b6d414521f0a519d819df32222d83 AS build
WORKDIR /workspace
COPY go.work ./
COPY platform ./platform
COPY services/tenant-service ./services/tenant-service
WORKDIR /workspace/services/tenant-service
# The Compose integration profile runs this build stage on the private Docker
# network. Keep module artifacts in the stage so it does not need public DNS
# merely to execute a PostgreSQL integration test.
RUN GOWORK=off go mod download
RUN --mount=type=cache,target=/go/pkg/mod --mount=type=cache,target=/root/.cache/go-build GOWORK=off go build -o /out/tenant-service ./cmd/api

FROM alpine:3.23@sha256:fd791d74b68913cbb027c6546007b3f0d3bc45125f797758156952bc2d6daf40
# IANA validation must behave identically in the service image and on the
# developer host. Alpine does not include the zoneinfo database by default.
RUN apk add --no-cache tzdata && adduser -D -H -u 10001 app
USER app
WORKDIR /app
COPY --from=build /out/tenant-service /app/tenant-service
COPY --from=build /workspace/services/tenant-service/migrations /app/migrations
ENTRYPOINT ["/app/tenant-service"]
