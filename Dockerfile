FROM golang:1.24.7-bookworm AS builder

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 go build \
    -trimpath \
    -ldflags="-s -w -buildid=" \
    -o /out/subsing .

FROM alpine:3.22

RUN apk add --no-cache ca-certificates

COPY --from=builder /out/subsing /usr/local/bin/subsing

WORKDIR /workdir
ENTRYPOINT ["/usr/local/bin/subsing"]
CMD ["/workdir", "/processed"]
