FROM golang:1.26.6-alpine@sha256:3889b425f035be855a72fb4755265311293b6d414521f0a519d819df32222d83 AS build
WORKDIR /workspace
COPY go.work ./
COPY platform ./platform
COPY services/notification-service ./services/notification-service
WORKDIR /workspace/services/notification-service
RUN GOWORK=off go mod download all
RUN --mount=type=cache,target=/go/pkg/mod --mount=type=cache,target=/root/.cache/go-build GOWORK=off go build -o /out/service ./cmd/api
FROM alpine:3.23@sha256:fd791d74b68913cbb027c6546007b3f0d3bc45125f797758156952bc2d6daf40
RUN adduser -D -H -u 10001 app
USER app
COPY --from=build /out/service /service
COPY --from=build /workspace/services/notification-service/migrations /migrations
ENTRYPOINT ["/service"]
