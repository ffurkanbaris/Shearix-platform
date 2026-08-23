FROM golang:1.26-alpine AS build
WORKDIR /workspace
COPY go.work ./
COPY platform ./platform
COPY services/catalog-service ./services/catalog-service
WORKDIR /workspace/services/catalog-service
RUN GOWORK=off go mod download all
RUN --mount=type=cache,target=/go/pkg/mod --mount=type=cache,target=/root/.cache/go-build GOWORK=off go build -o /out/service ./cmd/api
FROM alpine:3.21
RUN adduser -D -H -u 10001 app
USER app
WORKDIR /app
COPY --from=build /out/service /app/service
COPY --from=build /workspace/services/catalog-service/migrations /app/migrations
ENTRYPOINT ["/app/service"]
