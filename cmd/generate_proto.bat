cd ..

mkdir server\proto
mkdir client\proto

pause
echo go install google.golang.org/protobuf/cmd/protoc-gen-go@latest
echo go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@latest
echo npm install -g protoc-gen-js
echo npm install -g protoc-gen-grpc-web

protoc -I=proto --go_out=server --go-grpc_out=server proto/car.proto

echo protoc -I=proto --go_out=server/proto --go_opt=paths=source_relative --go-grpc_out=server/proto --go-grpc_opt=paths=source_relative proto\car.proto
protoc -I=proto   --js_out=import_style=commonjs:client\proto   --grpc-web_out=import_style=commonjs,mode=grpcwebtext:client\proto   proto\car.proto
