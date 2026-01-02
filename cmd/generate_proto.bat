cd ..

mkdir server\proto
mkdir humanplayer\proto

mkdir client\proto
mkdir AIWebPlayer\proto


echo go install google.golang.org/protobuf/cmd/protoc-gen-go@latest
echo go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@latest
echo npm install -g protoc-gen-js
echo npm install -g protoc-gen-grpc-web

protoc -I=proto --go_out=server --go-grpc_out=server proto/car.proto
protoc -I=proto proto/car.proto  --js_out=import_style=commonjs,binary:client/proto   --grpc-web_out=import_style=commonjs,mode=grpcwebtext:client/proto
xcopy /Y /E /I ".\client\proto\*" ".\humanplayer\proto\"

 
protoc -I=proto proto/car.proto   --js_out=import_style=commonjs:AIWebPlayer/proto   --grpc-web_out=import_style=commonjs,mode=grpcwebtext:AIWebPlayer/proto 