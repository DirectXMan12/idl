//go:generate protoc -I=../.. -I=/opt/protoc/include --go_out=. --go_opt=paths=source_relative ../../groupver.proto
package groupver
