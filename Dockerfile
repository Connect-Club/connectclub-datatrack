FROM golang:1.16

WORKDIR /go/src/app
COPY ./src .

RUN go build -o app .

CMD ["/go/src/app/app"]
