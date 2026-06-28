# ─── Build ───
FROM golang:1.22-alpine AS build
WORKDIR /src
COPY . .
RUN go mod download
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/engine ./cmd/engine

# ─── Runtime ───
FROM alpine:3.20
RUN apk add --no-cache ca-certificates tzdata && adduser -D -u 10001 mail
COPY --from=build /out/engine /usr/local/bin/engine
USER mail
EXPOSE 8080
ENTRYPOINT ["engine"]
