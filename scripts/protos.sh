#!/bin/sh

repo=$(git rev-parse --show-toplevel)

protoPath="$repo/proto"
goImports="-I=$GOPATH/src"

GO111MODULE=off go get github.com/googleapis/googleapis/...

go install \
	github.com/grpc-ecosystem/grpc-gateway/v2/protoc-gen-grpc-gateway \
	github.com/grpc-ecosystem/grpc-gateway/v2/protoc-gen-openapiv2 \
	google.golang.org/protobuf/cmd/protoc-gen-go \
	google.golang.org/grpc/cmd/protoc-gen-go-grpc

protoc $goImports -I$protoPath -I$GOPATH/src/github.com/googleapis/googleapis:$GOPATH/src/github.com/grpc-ecosystem/grpc-gateway/ \
			--go_out=$GOPATH/src \
			--go-grpc_out=$GOPATH/src \
			--grpc-gateway_out=logtostderr=true,request_context=true:$GOPATH/src \
			--openapiv2_out=logtostderr=true:$protoPath nest-stream.proto

mkdir -p internal/swagger
mv $protoPath/nest-stream.swagger.json internal/swagger/swagger.json
cat << EOF > internal/swagger/embed.go
package swagger

import "embed"

//go:embed *
// Static is an embedded file server containing static HTTP assets.
var Static embed.FS
EOF
