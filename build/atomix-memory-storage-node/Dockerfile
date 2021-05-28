FROM alpine:3.9

USER nobody

ADD build/_output/atomix-memory-storage-node /usr/local/bin/atomix-memory-storage-node

ENTRYPOINT ["atomix-memory-storage-node"]
