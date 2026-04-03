# ── 构建阶段 ──────────────────────────────────────────────────────────────
FROM golang:1.22-alpine AS builder

RUN apk add --no-cache git openssh-client

WORKDIR /build
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
    go build -ldflags="-w -s" -o sports-matcher cmd/server/main.go

# ── 运行阶段 ──────────────────────────────────────────────────────────────
FROM alpine:3.19

RUN apk add --no-cache ca-certificates tzdata openssh-client

WORKDIR /app
COPY --from=builder /build/sports-matcher .

# SSH 私钥挂载点（运行时通过 volume 或 secret 注入）
VOLUME ["/app/keys"]

EXPOSE 8080

# 环境变量（可通过 docker run -e 或 .env 文件覆盖）
ENV SSH_HOST=""
ENV SSH_PORT="22"
ENV SSH_USER=""
ENV SSH_KEY_PATH="/app/keys/id_rsa"
ENV DB_HOST=""
ENV DB_PORT="3306"
ENV DB_USER=""
ENV DB_PASSWORD=""
ENV SERVER_PORT="8080"

ENTRYPOINT ["/app/sports-matcher"]
CMD ["serve", "--port", "8080"]
