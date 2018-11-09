package truss

import (
	"github.com/frankee/truss/svcdef"
	"github.com/frankee/truss/truss/execprotoc"
	"io"
)

// Config defines the inputs to a truss service generation
type Config struct {
	// The first path in $GOPATH
	GoPath []string

	// The go package where .pb.go files protoc-gen-go creates will be written
	PBGoPackage string
	PBGoPath    string
	// The go package where the service code will be written
	ServicePackage string
	ServicePath    string

	// The paths to each of the .proto files truss is being run against
	DefPaths []string
	RawPaths []string

	// The files of a previously generated service, may be nil
	PrevGen map[string]io.Reader

	IncludePaths []string

	SvcPath string
	SvcDef *svcdef.Svcdef

	MetaInfos map[string]*execprotoc.ProtoMetaInfo
}
