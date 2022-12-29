# build stage
FROM golang:1-alpine AS build-env
RUN apk add git gcc libc-dev
ADD . /git
WORKDIR /git
RUN go test ./...
# Configure insteadOf so that Go's call to git clones the source code from the
# local git repo on disk. We currently need to do this in order to make the "go
# get" below get the code from a git checkout, so that build info is correctly
# populated. See
# https://pkg.go.dev/github.com/prometheus/client_golang/prometheus#NewBuildInfoCollector
RUN git config --global url."/git".insteadOf "https://github.com/G-Research/prommsd"
# Store the commit ID that this tree is at, so that our checkout later matches
# where this tree is. This means uncommited changes won't be part of the build.
RUN echo $(git describe --always) > /git-commit-id

WORKDIR /src
# Set GOPRIVATE to avoid any module proxy and therefore force our "insteadOf"
# version to be used, to avoid pulling from GitHub again (and make this build
# work for forks, etc).
ENV GOPRIVATE="github.com/G-Research/prommsd"
RUN go install github.com/G-Research/prommsd/cmd/prommsd@$(cat /git-commit-id)

# final stage
FROM alpine
WORKDIR /app
COPY --from=build-env /go/bin/prommsd .
USER 1000
ENTRYPOINT ["/app/prommsd"]
