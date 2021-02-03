module k8s.io/idl/backends/common

go 1.15

require (
	github.com/go-logr/logr v0.4.0
	github.com/go-logr/zapr v0.4.0
	github.com/golang/protobuf v1.4.3
	go.uber.org/zap v1.16.0
	google.golang.org/protobuf v1.25.0
	k8s.io/idl/ckdl-ir/goir v0.0.0-00010101000000-000000000000
)

replace k8s.io/idl/ckdl-ir/goir => ../../ckdl-ir/goir
