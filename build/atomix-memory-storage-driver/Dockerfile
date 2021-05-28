FROM alpine:3.9

USER nobody

ADD build/_output/atomix-memory-storage-driver /usr/local/bin/atomix-memory-storage-driver

ENTRYPOINT ["atomix-memory-storage-driver"]
