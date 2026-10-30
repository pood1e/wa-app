# syntax=docker/dockerfile:1
FROM --platform=$BUILDPLATFORM node:24-bookworm-slim AS dashboard
RUN apt-get update && apt-get install -y --no-install-recommends protobuf-compiler libprotobuf-dev ca-certificates && rm -rf /var/lib/apt/lists/*
WORKDIR /src
COPY proto proto
COPY scripts scripts
COPY webui/package*.json webui/
RUN npm --prefix webui ci --no-audit --no-fund
COPY webui webui
RUN npm --prefix webui run build

FROM --platform=$BUILDPLATFORM golang:1.27.2-alpine AS source
RUN apk add --no-cache ca-certificates protobuf-dev
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
RUN go install google.golang.org/protobuf/cmd/protoc-gen-go@v1.36.11 && go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@v1.6.2
COPY proto proto
COPY scripts scripts
RUN scripts/generate-proto.sh
COPY internal internal
COPY cmd cmd

FROM source AS runtime-build
ARG TARGETOS
ARG TARGETARCH
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -trimpath -o /out/wa-app-service ./cmd/wa-app-service
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -trimpath -o /out/wa-migrate ./cmd/wa-migrate

FROM source AS account-add-build
ARG TARGETOS
ARG TARGETARCH
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -trimpath -o /out/wa-account-add-service ./cmd/wa-account-add-service

FROM alpine:3.24.2 AS base
RUN apk add --no-cache ca-certificates && addgroup -S -g 10001 app && adduser -S -u 10001 -G app app && mkdir -p /var/lib/wa-app && chown app:app /var/lib/wa-app
WORKDIR /app
USER app

FROM base AS account-add
COPY --from=account-add-build /out/wa-account-add-service /app/
EXPOSE 50092
HEALTHCHECK --interval=15s --timeout=5s CMD ["/app/wa-account-add-service", "health"]
ENTRYPOINT ["/app/wa-account-add-service"]

FROM base AS runtime
COPY --from=runtime-build /out/wa-app-service /out/wa-migrate /app/
COPY --from=dashboard /src/webui/dist /app/dashboard/wa
EXPOSE 50091 8080
HEALTHCHECK --interval=15s --timeout=5s CMD ["/app/wa-app-service", "health"]
ENTRYPOINT ["/app/wa-app-service"]
