FROM alpine

WORKDIR "/opt"
ADD ./config.yml config.yml
ADD ./p2p p2p
RUN chmod +x /opt/p2p

ENTRYPOINT ["/opt/p2p"]