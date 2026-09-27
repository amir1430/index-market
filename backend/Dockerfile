FROM golang:1.26-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -ldflags="-s -w" -o /indexd .

# distroless/static ships CA certs, needed for wss:// to exchanges
FROM gcr.io/distroless/static-debian12
COPY --from=build /indexd /indexd
EXPOSE 8080
ENTRYPOINT ["/indexd"]
