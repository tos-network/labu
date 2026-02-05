FROM golang:1.22-alpine AS builder
WORKDIR /source
COPY go.mod go.sum /source/
COPY labusim /source/labusim
COPY simulators/tos/consensus /source/simulators/tos/consensus
WORKDIR /source/simulators/tos/consensus
RUN go build -o /sim .

FROM alpine:latest
RUN apk add --no-cache ca-certificates
COPY --from=builder /sim /sim
ENTRYPOINT ["/sim"]
