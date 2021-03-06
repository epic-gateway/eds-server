FROM golang:alpine as builder

ENV GOOS=linux
ARG GITLAB_AUTHN

# install and configure git. we need it because some of our modules
# (e.g., egw-resource-model) are private
RUN apk add git
RUN git config --global url."https://${GITLAB_AUTHN}@gitlab.com/acnodal".insteadOf https://gitlab.com/acnodal

WORKDIR /usr/src/epic-eds
COPY . ./

# build the executable (static)
RUN go build  -tags 'osusergo netgo' -o /usr/local/bin/epic-eds ./main.go


# start fresh
FROM ubuntu:20.04

# install dependencies
RUN apt-get update \
    && apt-get install -y libelf-dev \
    && rm -rf /var/lib/apt/lists/*

# copy executables from the builder image
WORKDIR /usr/local/bin
COPY --from=builder /usr/local/bin/epic-eds ./

EXPOSE 8080
CMD ["/usr/local/bin/epic-eds", "--debug"]
