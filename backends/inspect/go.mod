module k8s.io/idl/backends/inspect

go 1.15

replace (
	k8s.io/idl/backends/common => ../common
	k8s.io/idl/ckdl-ir/goir => ../../ckdl-ir/goir
)

require (
	github.com/golang/protobuf v1.4.3
	google.golang.org/protobuf v1.25.0
	k8s.io/idl/backends/common v0.0.0-00010101000000-000000000000
	k8s.io/idl/ckdl-ir/goir v0.0.0-00010101000000-000000000000
	sigs.k8s.io/yaml v1.2.0
)
