FROM ubuntu:14.04

COPY ./sonyflake_server /sonyflake_server

RUN useradd -m sonyflake
USER sonyflake

ENTRYPOINT ["/sonyflake_server"]
EXPOSE 8080
