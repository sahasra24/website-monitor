FROM golang:1.22 AS build
WORKDIR /src
COPY go.mod ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /out/website-monitor ./cmd/monitor

FROM gcr.io/distroless/static-debian12:nonroot
WORKDIR /app
COPY --from=build /out/website-monitor /website-monitor
VOLUME ["/data"]
ENV DATABASE_PATH=/data/monitor.db
EXPOSE 8080
ENTRYPOINT ["/website-monitor"]
