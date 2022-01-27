FROM golang:1.17-alpine as build
WORKDIR /go/src/github.com/bilbercode/nest-proxy
COPY . .
RUN go mod download -x
RUN go build -o nest-proxy *.go

FROM alpine
RUN apk --no-cache add ca-certificates
WORKDIR /app
COPY --from=build /go/src/github.com/bilbercode/nest-proxy/nest-proxy /app/nest-proxy
RUN mkdir -p /app/data
ENTRYPOINT exec /app/nest-proxy
