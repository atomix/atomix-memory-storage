FROM alpine:3.9

USER nobody

ADD build/_output/atomix-memory-storage-controller /usr/local/bin/atomix-memory-storage-controller

ENTRYPOINT ["atomix-memory-storage-controller"]
