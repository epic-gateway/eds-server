FROM golang:1.17-alpine as builder

ENV GOOS=linux
ARG GITLAB_USER
ARG GITLAB_PASSWORD

# install and configure git. we need it because some of our modules
# (e.g., epic/resource-model) are private
RUN apk add git
RUN echo "machine gitlab.com login ${GITLAB_USER} password ${GITLAB_PASSWORD}" > ~/.netrc

WORKDIR /opt/acnodal/src
COPY . ./

# build the executable (static)
RUN go build  -tags 'osusergo netgo' -o ../bin/eds-server main.go


# start fresh
FROM golang:1.17-alpine
ENV bin=/opt/acnodal/bin/eds-server

# copy executables from the builder image
COPY --from=builder ${bin} ${bin}

EXPOSE 18000

# The softlink is because Dockerfile variable interpolation happens at
# run-time so if you have variables in the CMD string they won't get
# resolved to their values.  This lets us have a hard-coded CMD string
# that links to the image-specific command.
RUN ln -s ${bin} /opt/acnodal/bin/cmd
CMD ["/opt/acnodal/bin/cmd"]
