# Docker image for the Drone build runner
#
#     CGO_ENABLED=0 go build -a -tags netgo
#     docker build --rm=true -t drone/drone-cache .

FROM gliderlabs/alpine:3.1
ADD drone-cache /bin/
ENTRYPOINT ["/bin/drone-cache"]
