# ── 构建阶段 ──
FROM golang:1.27-alpine AS build

WORKDIR /src

# 先只拷贝依赖清单，让依赖层可以被缓存
COPY go.mod go.sum* ./
RUN go mod download

COPY . .

# 静态链接，产出不依赖 libc 的二进制，便于放进 distroless 运行
RUN CGO_ENABLED=0 GOOS=linux go build \
        -trimpath \
        -ldflags="-s -w" \
        -o /out/majspirit \
        ./cmd/server

# ── 运行阶段 ──
FROM gcr.io/distroless/static-debian12:nonroot

COPY --from=build /out/majspirit /majspirit

EXPOSE 8080
USER nonroot:nonroot

ENTRYPOINT ["/majspirit"]
