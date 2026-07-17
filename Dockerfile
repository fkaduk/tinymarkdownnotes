FROM golang:1.22-bookworm AS build

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY main.go ./
RUN CGO_ENABLED=1 go build -o /out/tinymarkdownnotes .

FROM debian:bookworm-slim

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
