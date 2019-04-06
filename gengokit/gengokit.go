package gengokit

import (
	"bytes"
	"github.com/frankee/truss/gengokit/genutil"
	"github.com/serenize/snaker"
	"io"
	"strings"
	"text/template"
	"unicode"

	generatego "github.com/golang/protobuf/protoc-gen-go/generator"
	"github.com/pkg/errors"

	"github.com/frankee/truss/gengokit/clientarggen"
	"github.com/frankee/truss/gengokit/httptransport"
	"github.com/frankee/truss/svcdef"
)

type Renderable interface {
	Render(string, *Data) (io.Reader, error)
}

type Config struct {
	GoPackage   string
	PBGoPackage string
	Version     string
	VersionDate string

	PreviousFiles map[string]io.Reader
}

// lowCamelName returns a CamelCased string, but with the first letter
// lowercased. "example_name" becomes "exampleName".
func ToLowCamelName(s string) string {
	s = generatego.CamelCase(s)
	new := []rune(s)
	if len(new) < 1 {
		return s
	}
	rv := []rune{}
	rv = append(rv, unicode.ToLower(new[0]))
	rv = append(rv, new[1:]...)
	return string(rv)
}

// FuncMap contains a series of utility functions to be passed into
// templates and used within those templates.
var FuncMap = template.FuncMap{
	"ToLower":        strings.ToLower,
	"GoName":         generatego.CamelCase,
	"ToLowCamelName": ToLowCamelName,
	"ToSnake":        snaker.CamelToSnake,
	"PackageName":    genutil.GetPackageName,
}

// Data is passed to templates as the executing struct; its fields
// and methods are used to modify the template
type Data struct {
	// import path for the directory containing the definition .proto files
	ImportPath string
	// import path for .pb.go files containing service structs
	PBImportPath string

	ExternalMessageImports []string

	// PackageName is the name of the package containing the service definition
	PackageName string
	// GRPC/Protobuff service, with all parameters and return values accessible
	Service    *svcdef.Service
	ClientArgs *clientarggen.ClientServiceArgs
	// A helper struct for generating http transport functionality.
	HTTPHelper *httptransport.Helper
	FuncMap    template.FuncMap

	Version     string
	VersionDate string
}

func NewData(sd *svcdef.Svcdef, conf Config) (*Data, error) {
	return &Data{
		ImportPath:             conf.GoPackage,
		PBImportPath:           conf.PBGoPackage,
		PackageName:            sd.PbPkgName,
		Service:                sd.Service,
		ExternalMessageImports: sd.ImportPaths,
		ClientArgs:             clientarggen.New(sd.Service),
		HTTPHelper:             httptransport.NewHelper(sd.Service),
		FuncMap:                FuncMap,
		Version:                conf.Version,
		VersionDate:            conf.VersionDate,
	}, nil
}

// ApplyTemplate applies the passed template with the Data
func (e *Data) ApplyTemplate(templ string, templName string) (io.Reader, error) {
	return ApplyTemplate(templ, templName, e, e.FuncMap)
}

// ApplyTemplate is a helper methods that packages can call to render a
// template with any data and func map
func ApplyTemplate(templ string, templName string, data interface{}, funcMap template.FuncMap) (io.Reader, error) {
	codeTemplate, err := template.New(templName).Funcs(funcMap).Parse(templ)
	if err != nil {
		return nil, errors.Wrap(err, "cannot create template")
	}

	outputBuffer := bytes.NewBuffer(nil)
	err = codeTemplate.Execute(outputBuffer, data)
	if err != nil {
		return nil, errors.Wrap(err, "template error")
	}

	return outputBuffer, nil
}
