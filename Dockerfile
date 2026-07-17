FROM golang:1.26.5-bookworm AS build

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY main.go ./
RUN CGO_ENABLED=1 go build -trimpath -ldflags="-s -w" -o /out/tinymarkdownnotes .

FROM debian:12-slim

WORKDIR /app
COPY --from=build /out/tinymarkdownnotes /app/tinymarkdownnotes
COPY templates/ templates/
COPY static/ static/
RUN mkdir -p data notes \
    && useradd -m appuser \
    && chown -R appuser:appuser /app
USER appuser

EXPOSE 5000
CMD ["/app/tinymarkdownnotes"]
