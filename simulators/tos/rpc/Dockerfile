FROM golang:1.22-alpine AS builder
RUN apk add --no-cache git
WORKDIR /source
COPY go.mod go.sum /source/
COPY labusim /source/labusim
COPY simulators/tos/rpc /source/simulators/tos/rpc
WORKDIR /source/simulators/tos/rpc
RUN go build -o /sim .

FROM alpine:latest
RUN apk add --no-cache ca-certificates curl
COPY --from=builder /sim /sim
ENTRYPOINT ["/sim"]
