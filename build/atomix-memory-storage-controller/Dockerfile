FROM alpine:3.9

USER nobody

ADD build/cache-storage-controller/_output/cache-storage-controller /usr/local/bin/cache-storage-controller

ENTRYPOINT ["cache-storage-controller"]
