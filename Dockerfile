FROM gcc:13 AS cross

ARG TARGETPLATFORM

RUN apt-get update && apt-get install -y cmake
