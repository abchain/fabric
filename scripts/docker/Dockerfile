FROM abchain/fabric:base

RUN mkdir -p $GOPATH/src/github.com/abchain \
    && cd $GOPATH/src/github.com/abchain \
    && git clone https://github.com/abchain/fabric

RUN go get -u github.com/golang/protobuf/protoc-gen-go
RUN go get -u google.golang.org/grpc 
RUN go get golang.org/x/crypto golang.org/x/net golang.org/x/crypto golang.org/x/text

WORKDIR $GOPATH/bin
