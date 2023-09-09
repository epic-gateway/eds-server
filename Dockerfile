FROM golang:1.17.13-alpine as builder

ENV GOOS=linux
WORKDIR /opt/epic-gateway/src
COPY . ./

# build the executable (static)
RUN go build  -tags 'osusergo netgo' -o ../bin/eds-server main.go


# start fresh
FROM alpine:3.16.7

# copy executable from the builder image
ENV bin=/opt/epic-gateway/bin/eds-server
COPY --from=builder ${bin} ${bin}

EXPOSE 18000

CMD ["/opt/epic-gateway/bin/eds-server"]
