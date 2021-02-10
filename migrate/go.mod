module k8s.io/idl/migrate

go 1.15

require (
	github.com/golang/protobuf v1.4.3
	github.com/spf13/cobra v1.1.1
	github.com/spf13/pflag v1.0.5 // indirect
	google.golang.org/protobuf v1.25.0
	k8s.io/apiextensions-apiserver v0.20.2
	k8s.io/apimachinery v0.20.2
	k8s.io/idl/ckdl-ir/goir v0.0.0-00010101000000-000000000000
	sigs.k8s.io/controller-tools v0.4.1
)

replace (
    k8s.io/idl/ckdl-ir/goir => ../ckdl-ir/goir
	sigs.k8s.io/controller-tools => /tmp/ct-main
)
