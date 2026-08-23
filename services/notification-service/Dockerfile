FROM golang:1.26-alpine AS build
WORKDIR /workspace
COPY go.work ./
COPY platform ./platform
COPY services/notification-service ./services/notification-service
WORKDIR /workspace/services/notification-service
RUN GOWORK=off go mod download all
RUN --mount=type=cache,target=/go/pkg/mod --mount=type=cache,target=/root/.cache/go-build GOWORK=off go build -o /out/service ./cmd/api
FROM alpine:3.21
RUN adduser -D -H -u 10001 app
USER app
COPY --from=build /out/service /service
COPY --from=build /workspace/services/notification-service/migrations /migrations
ENTRYPOINT ["/service"]
