# ─── Build ───
FROM golang:1.22-alpine AS build
WORKDIR /src
COPY . .
RUN go mod tidy
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/engine ./cmd/engine

# ─── Runtime ───
FROM alpine:3.20
RUN apk add --no-cache ca-certificates tzdata && adduser -D -u 10001 engine
COPY --from=build /out/engine /usr/local/bin/engine
USER engine
EXPOSE 8080
ENTRYPOINT ["engine"]
