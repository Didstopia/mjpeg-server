# Compiler image
FROM didstopia/base:go-alpine-3.14 AS go-builder

# Copy the project 
COPY . /tmp/mjpeg-server/
WORKDIR /tmp/mjpeg-server/

# Install dependencies
RUN apk add --no-cache protobuf && \
    make deps

# Build the binary
#RUN make build && ls /tmp/mjpeg-server
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -a -installsuffix cgo -ldflags="-w -s" -o /go/bin/mjpeg-server



# Runtime image
FROM scratch

# Copy certificates
COPY --from=go-builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt

# Copy the built binary
COPY --from=go-builder /go/bin/mjpeg-server /go/bin/mjpeg-server

# Expose environment variables
ENV MJPEG_SERVER_ADDRESS_WEB       ":8080"
ENV MJPEG_SERVER_ADDRESS_UDP       ":8081"
ENV MJPEG_SERVER_FRAMERATE         "25"

# Expose ports
EXPOSE 8080/tcp
EXPOSE 8081/udp

# Run the binary
ENTRYPOINT ["/go/bin/mjpeg-server"]
