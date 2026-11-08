FROM node:22-alpine AS frontend
WORKDIR /web
COPY web/package*.json ./
RUN npm ci
COPY web/ ./
RUN npm run build

FROM golang:1.27.3-alpine AS builder
RUN apk add --no-cache ca-certificates
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd/ cmd/
COPY internal/ internal/
COPY --from=frontend /internal/ui/dist/ internal/ui/dist/
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /gateway ./cmd/gateway \
 && mkdir -p /data && chown 65532:65532 /data && chmod 700 /data

FROM scratch
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=builder /gateway /gateway
COPY --from=builder --chown=65532:65532 /data /data
USER 65532:65532
WORKDIR /data
ENTRYPOINT ["/gateway"]
