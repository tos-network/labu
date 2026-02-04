FROM golang:1.22-alpine AS builder
WORKDIR /source
COPY go.mod go.sum /source/
COPY labsim /source/labsim
COPY simulators/tos/execution /source/simulators/tos/execution
WORKDIR /source/simulators/tos/execution
RUN go build -o /sim .

FROM alpine:latest
RUN apk add --no-cache ca-certificates
COPY --from=builder /sim /sim
ENTRYPOINT ["/sim"]
