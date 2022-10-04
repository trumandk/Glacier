FROM golang:1.17 as builder

WORKDIR /app/

COPY go.mod go.mod
COPY go.sum go.sum
RUN go mod download

COPY do_not_compress.go do_not_compress.go
COPY main.go main.go
RUN CGO_ENABLED=0 go build -o /main
RUN chmod 777 /main

FROM scratch
COPY static /static
COPY --from=builder /main /main
ENTRYPOINT ["/main"]
