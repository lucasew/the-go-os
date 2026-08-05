package control

// Generate protobuf types into pkg/control/v1.
// Requires protoc on PATH. Plugin: go tool protoc-gen-go (see go.mod tool stanza).
//
//go:generate sh -c "protoc -I ../../api --go_out=../.. --go_opt=module=github.com/lucasew/the-go-os --plugin=protoc-gen-go=$(go tool -n protoc-gen-go) ../../api/control/v1/control.proto"
