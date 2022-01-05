FROM golang:1.17-alpine AS builder

WORKDIR /go/src/github.com/openfaas-incubator/faas-idler

COPY main.go    main.go
COPY go.mod     go.mod
COPY vendor     vendor

RUN go build -o /usr/bin/faas-idler .

FROM alpine:3.7

RUN addgroup -S app && adduser -S -g app app
RUN mkdir -p /home/app

WORKDIR /home/app

COPY --from=builder /usr/bin/faas-idler /home/app/

RUN chown -R app /home/app
USER app
RUN mkdir -p /home/app/podLog


EXPOSE 8080
VOLUME /tmp

ENTRYPOINT ["/home/app/faas-idler"]