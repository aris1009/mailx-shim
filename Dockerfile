FROM docker.io/library/golang:1.23-alpine AS builder
WORKDIR /src
COPY go.mod ./
COPY *.go ./
RUN CGO_ENABLED=0 go build -ldflags="-s -w" -o /mailx-shim .

FROM scratch
COPY --from=builder /mailx-shim /mailx-shim
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
ENTRYPOINT ["/mailx-shim"]
