FROM docker.io/golang:1.20-bullseye AS builder

ENV GO111MODULE=on \
    CGO_ENABLED=0 \
    GOOS=linux \
    GOARCH=amd64

WORKDIR /build
COPY ivcap_service.go zap_logger.go go.mod go.sum ./

WORKDIR /build/examples/text_on_image
COPY examples/text_on_image/go.mod examples/text_on_image/go.sum ./
RUN go mod download

# Copy all relavant files into container
COPY examples/text_on_image/Makefile ./
COPY examples/text_on_image/main.go ./

# Build the application
ARG GIT_COMMIT
ARG GIT_TAG
ARG SERVICE_ID
RUN make GIT_COMMIT=$GIT_COMMIT GIT_TAG=$GIT_TAG SERVICE_ID=$SERVICE_ID build

# # Setup for smaller image
WORKDIR /dist
RUN cp /build/examples/text_on_image/text-on-image .


# 
FROM scratch
COPY --from=builder /dist/text-on-image /
COPY examples/text_on_image/CaveatBrush-Regular.ttf /
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/

# 
# Command to run
ENTRYPOINT ["/text-on-image"]
