module k8s.io/idl/backends/tocrd

go 1.15

replace k8s.io/idl/ckdl-ir/goir => ../../ckdl-ir/goir

require (
	github.com/gobuffalo/flect v0.2.2
	github.com/golang/protobuf v1.4.3
	github.com/google/go-cmp v0.5.4
	github.com/onsi/ginkgo v1.14.2
	github.com/onsi/gomega v1.10.4
	golang.org/x/tools v0.0.0-20210113180300-f96436850f18
	k8s.io/apiextensions-apiserver v0.20.2
	k8s.io/apimachinery v0.20.2
	k8s.io/idl/ckdl-ir/goir v0.0.0-00010101000000-000000000000
	sigs.k8s.io/controller-tools v0.4.1
	sigs.k8s.io/yaml v1.2.0
)
