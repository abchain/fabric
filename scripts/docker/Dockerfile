FROM ubuntu:xenial AS base

###############################
## 1. preinstall
RUN apt-get -y update
RUN apt-get -y install software-properties-common curl wget
# Update the entire system to the latest releases
RUN apt-get update -qq
RUN apt-get dist-upgrade -qqy
RUN apt-get install --yes git
RUN apt-get install --yes sudo
RUN apt-get install -y autoconf automake libtool curl make g++ unzip build-essential libsnappy-dev zlib1g-dev libbz2-dev

FROM base AS goinstall

ARG GO_VERSION=1.10.8
RUN mkdir -p /tmp/golang \
    && cd /tmp/golang \
    && wget --quiet https://studygolang.com/dl/golang/go${GO_VERSION}.linux-amd64.tar.gz \
    && tar -zvxf go${GO_VERSION}.linux-amd64.tar.gz

FROM base AS buildbase

ENV GOROOT=/opt/go
ENV GOPATH=/opt/gopath
ENV PATH=$PATH:${GOROOT}/bin:${GOPATH}/bin
COPY --from=goinstall /tmp/golang /opt/
RUN mkdir -p /opt/gopath && chmod a+rw -R ${GOPATH}

###############################
## 2. install rocksdb
FROM base AS dbinstall
ARG ROCKSDB_VERSION=5.17.2
RUN mkdir -p /tmp/rocksdb \
    && cd /tmp/rocksdb \
    && wget https://github.com/facebook/rocksdb/archive/v${ROCKSDB_VERSION}.tar.gz \
    && tar -zxvf v${ROCKSDB_VERSION}.tar.gz \
    && cd rocksdb-${ROCKSDB_VERSION} \
    && PORTABLE=1 make shared_lib -j2 \
    && INSTALL_PATH=/tmp/rocksdbinst make install-shared

FROM buildbase AS coderepo
RUN go get -u github.com/golang/protobuf/protoc-gen-go
######## go-around GFW ########
RUN mkdir -p ${GOPATH}/src/google.golang.org \
    && cd ${GOPATH}/src/google.golang.org \
    && git clone https://github.com/googleapis/go-genproto genproto \
    && git clone https://github.com/grpc/grpc-go.git grpc \
    && cd grpc \
    && git checkout v1.18.1
RUN mkdir -p ${GOPATH}/src/golang.org/x \
    && cd ${GOPATH}/src/golang.org/x \
    && git clone https://github.com/golang/net.git \
    && git clone https://github.com/golang/sys.git \
    && git clone https://github.com/golang/text.git \
    && git clone https://github.com/golang/crypto.git

RUN rm -rf $(find ${GOPATH}/src -name .git -type d)

FROM buildbase

COPY --from=dbinstall /tmp/rocksdbinst /usr/local
RUN ldconfig -v
COPY --from=coderepo ${GOPATH}/src ${GOPATH}/src/
RUN go get -d github.com/abchain/fabric/node \
    && cd ${GOPATH}/src/github.com/abchain/fabric \
    && git checkout 0.97
