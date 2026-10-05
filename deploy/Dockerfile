FROM golang:1.23-alpine AS build
WORKDIR /src
COPY go.mod go.sum* ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /out/products-api ./cmd/products-api

FROM alpine:3.20
COPY --from=build /out/products-api /usr/local/bin/products-api
EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/products-api"]
