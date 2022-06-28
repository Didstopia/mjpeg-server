# Compiler image
FROM didstopia/base:go-alpine-3.14 AS go-builder

# Copy the project 
COPY . /tmp/mjpeg-server/
WORKDIR /tmp/mjpeg-server/

# Install dependencies
RUN apk add --no-cache protobuf && \
    make deps

# Install idleproxy
RUN go install github.com/didstopia/mjpg-streamer-server/idleproxy@latest

# Build the binary
#RUN make build && ls /tmp/mjpeg-server
# RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -a -installsuffix cgo -ldflags="-w -s" -o /go/bin/mjpeg-server
RUN CGO_ENABLED=0 go build -a -installsuffix cgo -ldflags="-w -s" -o /go/bin/mjpeg-server



## FIXME: Fix scratch + ARM64 support (something about qemu + x86_64 + libmusl/musl)
# Runtime image
FROM scratch
# FROM didstopia/base:go-alpine-3.14

# Copy certificates
COPY --from=go-builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt

# Copy the mjpeg-server binary
COPY --from=go-builder /go/bin/mjpeg-server /go/bin/mjpeg-server

# Copy the idleproxy binary
COPY --from=go-builder /go/bin/idleproxy /go/bin/idleproxy

# Setup mjpeg-server environment variables
ENV MJPEG_SERVER_ADDRESS_WEB       ":8080"
ENV MJPEG_SERVER_ADDRESS_UDP       ":8081"
ENV MJPEG_SERVER_FRAMERATE         "25"

# Setup idleproxy environment variables
ENV IDLEPROXY_PROCESS_CWD "."
ENV IDLEPROXY_PROCESS_CMD "/go/bin/mjpeg-server"
ENV IDLEPROXY_DEBUG "false"

# Expose mjpeg-server ports
EXPOSE 8080/tcp
EXPOSE 8081/udp

# Expose idleproxy ports
EXPOSE 80/tcp

# Run mjpeg-server as the main entrypoint
# ENTRYPOINT ["/go/bin/mjpeg-server"]

## FIXME: The idleproxy should (optionally?) show both stdout and stderr for the (optional) daemon process,
##        however it is NOT showing any output for the process!

## TODO: The idleproxy should ALWAYS gracefully shutdown any daemon processes,
##       as I believe it might currently just be terminating them instead?!

# Run idleproxy as the main entrypoint
ENTRYPOINT ["/go/bin/idleproxy"]
