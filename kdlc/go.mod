module k8s.io/idl/kdlc

go 1.15

require (
	github.com/golang/protobuf v1.4.3
	google.golang.org/protobuf v1.25.0
	gopkg.in/yaml.v2 v2.4.0
	k8s.io/idl/ckdl-ir/goir v0.0.0-00010101000000-000000000000
)

replace k8s.io/idl/ckdl-ir/goir => ../ckdl-ir/goir