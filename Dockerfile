FROM golang:1.20.2-alpine3.17 AS builder
RUN apk update && apk add --no-cache git
WORKDIR $GOPATH/src/vmproxy/
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags="-w -s" -o /go/bin/vmproxy ./cmd/vmproxy
RUN apk del git

FROM scratch
COPY --from=builder /go/bin/vmproxy /go/bin/vmproxy
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
ENTRYPOINT ["/go/bin/vmproxy"]
