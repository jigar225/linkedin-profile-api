# ---- build stage ----
FROM golang:1.24-alpine AS build
WORKDIR /app

# deps first (layer caching: go.mod/go.sum change rarely)
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 go build -ldflags="-s -w" -o /bin/linkedin-profile-api ./cmd

# ---- runtime stage ----
FROM alpine:3.20
# CA certs are NOT in alpine by default — we're an HTTPS client, we need them
RUN apk add --no-cache ca-certificates && adduser -D -u 10001 appuser
USER appuser
COPY --from=build /bin/linkedin-profile-api /bin/linkedin-profile-api
EXPOSE 8080
# config via env only (LI_AT, JSESSIONID, PORT) — no files baked in
ENTRYPOINT ["/bin/linkedin-profile-api"]
