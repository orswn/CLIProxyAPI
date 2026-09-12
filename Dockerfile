FROM golang:1.27.1-alpine AS builder

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .

ARG VERSION=dev
ARG COMMIT=none
ARG BUILD_DATE=unknown

RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w -X 'main.Version=${VERSION}' -X 'main.Commit=${COMMIT}' -X 'main.BuildDate=${BUILD_DATE}'" -o /CLIProxyAPI ./cmd/server/

FROM gcr.io/distroless/static-debian12:nonroot

WORKDIR /app

COPY --from=builder /CLIProxyAPI /app/CLIProxyAPI
COPY config.example.yaml /app/config.example.yaml

EXPOSE 8317

USER nonroot:nonroot

ENTRYPOINT ["/app/CLIProxyAPI"]
