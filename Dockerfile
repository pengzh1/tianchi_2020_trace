FROM busybox

ARG BIN
COPY $BIN /bin
ENV GOGC off
ENV DEV 0
ENTRYPOINT /bin/tctrace