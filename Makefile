vendor-proto:
	@mkdir -p vendor.protogen/google/api
	@install -m 0644 $(shell go env GOPATH)/pkg/mod/github.com/grpc-ecosystem/grpc-gateway@v1.16.0/third_party/googleapis/google/api/annotations.proto vendor.protogen/google/api/
	@install -m 0644 $(shell go env GOPATH)/pkg/mod/github.com/grpc-ecosystem/grpc-gateway@v1.16.0/third_party/googleapis/google/api/http.proto vendor.protogen/google/api/

generate-wearable-proto: vendor-proto
	@mkdir -p pkg/api/wearable/v1
	@protoc \
		-I api \
		-I vendor.protogen \
		--go_out=pkg/api --go_opt=paths=source_relative \
		--go-grpc_out=pkg/api --go-grpc_opt=paths=source_relative \
		--grpc-gateway_out=pkg/api --grpc-gateway_opt=paths=source_relative \
		api/wearable/v1/wearable_service.proto

	