cd ..

mkdir server\proto
mkdir gocar\proto

mkdir humanplayer\proto
mkdir observer\proto


echo go install google.golang.org/protobuf/cmd/protoc-gen-go@latest
echo go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@latest
echo npm install -g protoc-gen-js
echo npm install -g protoc-gen-grpc-web

echo go protocol
protoc -I=proto --go_out=server --go-grpc_out=server proto/car.proto
xcopy /Y /E /I ".\server\proto\*" ".\gocar\proto\"

echo js protocol
protoc -I=proto proto/car.proto  --js_out=import_style=commonjs,binary:observer/proto   --grpc-web_out=import_style=commonjs,mode=grpcwebtext:observer/proto
xcopy /Y /E /I ".\observer\proto\*" ".\humanplayer\proto\"

  