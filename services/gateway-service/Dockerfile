FROM golang:1.26-alpine AS build
WORKDIR /workspace
COPY go.work ./
COPY platform ./platform
COPY services/gateway-service ./services/gateway-service
WORKDIR /workspace/services/gateway-service
RUN GOWORK=off go mod download all
RUN --mount=type=cache,target=/go/pkg/mod --mount=type=cache,target=/root/.cache/go-build GOWORK=off go build -o /out/gateway-service ./cmd/api

FROM alpine:3.21
RUN adduser -D -H -u 10001 app
USER app
COPY --from=build /out/gateway-service /gateway-service
ENTRYPOINT ["/gateway-service"]
