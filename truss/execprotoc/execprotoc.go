// Package execprotoc provides an interface for interacting with proto
// requiring only paths to files on disk
package execprotoc

import (
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	proto_parser "github.com/emicklei/proto"
	"github.com/golang/protobuf/proto"
	plugin "github.com/golang/protobuf/protoc-gen-go/plugin"
	"github.com/pkg/errors"
)

func withImport(apply func(p *proto_parser.Import)) proto_parser.Handler {
	return func(v proto_parser.Visitee) {
		if s, ok := v.(*proto_parser.Import); ok {
			apply(s)
		}
	}
}

func withPackage(apply func(p *proto_parser.Package)) proto_parser.Handler {
	return func(v proto_parser.Visitee) {
		if s, ok := v.(*proto_parser.Package); ok {
			apply(s)
		}
	}
}

type ProtoMetaInfo struct {
	IncludePath string
	FilePath    string
	FileName    string

	// go options in proto file
	PackagePath string
	PackageName string

	Imports []string

	ExternalMessages []string
}

func GetProtoMetaInfo(protofile string) *ProtoMetaInfo {
	reader, _ := os.Open(protofile)
	defer reader.Close()

	parser := proto_parser.NewParser(reader)
	definition, _ := parser.Parse()

	definedMessage := make(map[string]bool)
	var rpcMessages []string
	metaInfo := &ProtoMetaInfo{}
	proto_parser.Walk(definition, withImport(func(p *proto_parser.Import) {
		metaInfo.Imports = append(metaInfo.Imports, p.Filename)
		fmt.Println(p.Filename)
	}), proto_parser.WithOption(func(option *proto_parser.Option) {
		if option.Name == "go_package" {
			packages := strings.Split(option.Constant.Source, ";")
			if len(packages) == 2 {
				metaInfo.PackagePath = packages[0]
				metaInfo.PackageName = packages[1]
			} else if len(packages) == 1 {
				metaInfo.PackagePath = packages[0]
			}
		}
		fmt.Println(option)
	}), withPackage(func(p *proto_parser.Package) {
		metaInfo.PackagePath = p.Name
	}), proto_parser.WithMessage(func(message *proto_parser.Message) {
		definedMessage[message.Name] = true
	}), proto_parser.WithRPC(func(rpc *proto_parser.RPC) {
		rpcMessages = append(rpcMessages, rpc.RequestType)
		rpcMessages = append(rpcMessages, rpc.ReturnsType)
	}))

	for _, msg := range rpcMessages {
		if _, ok := definedMessage[msg]; !ok {
			metaInfo.ExternalMessages = append(metaInfo.ExternalMessages, msg)
		}
	}

	return metaInfo
}

func GetProtoImports(protofile string) []string {
	reader, _ := os.Open(protofile)
	defer reader.Close()

	parser := proto_parser.NewParser(reader)
	definition, _ := parser.Parse()

	importFiles := []string{}
	proto_parser.Walk(definition, withImport(func(p *proto_parser.Import) {
		importFiles = append(importFiles, p.Filename)
		fmt.Println(p.Filename)
	}), proto_parser.WithOption(func(option *proto_parser.Option) {
		fmt.Println(option)
	}))

	return importFiles
}

// GeneratePBDotGo creates .pb.go and .validator.pb.go files from the passed protoPaths and writes
// them to outDir.
func GeneratePBDotGo(protoPaths, gopath []string, outDir string) error {
	if len(outDir) == 0 {
		outDir = "."
	}

	genGoCode := "--go_out=" +
		"plugins=grpc:" +
		outDir

	_, err := exec.LookPath("protoc-gen-go")
	if err != nil {
		return errors.Wrap(err, "cannot find protoc-gen-go in PATH")
	}

	_, err = exec.LookPath("protoc-gen-govalidators")
	if err != nil {
		return errors.Wrap(err, "cannot find protoc-gen-govalidators in PATH")
	}

	genValidatorCode := "--govalidators_out=" + outDir

	err = protoc(protoPaths, gopath, []string{genGoCode, genValidatorCode})
	if err != nil {
		return errors.Wrap(err, "cannot exec protoc with protoc-gen-go")
	}

	return nil
}

// CodeGeneratorRequest returns a protoc CodeGeneratorRequest from running
// protoc on protoPaths
// TODO: replace getProtocOutput with some other way of getting the protoc ast.
// i.e. the binary data that will allow proto.Unmarshal to Unmashal the
// .proto file into a *plugin.CodeGeneratorRequest
func CodeGeneratorRequest(protoPaths, gopath []string) (*plugin.CodeGeneratorRequest, error) {
	protocOut, err := getProtocOutput(protoPaths, gopath)
	if err != nil {
		return nil, errors.Wrap(err, "cannot get output from protoc")
	}

	req := new(plugin.CodeGeneratorRequest)
	if err = proto.Unmarshal(protocOut, req); err != nil {
		return nil, errors.Wrap(err, "cannot marshal protoc ouput to code generator request")
	}

	return req, nil
}

// TODO: getProtocOutput is broken because golang protoc plugins no longer can
// have UTF-8 in the output. This caused protoc-gen-truss-protocast to fail to
// output its the protoc AST.
func getProtocOutput(protoPaths, gopath []string) ([]byte, error) {
	_, err := exec.LookPath("protoc-gen-truss-protocast")
	if err != nil {
		return nil, errors.Wrap(err, "protoc-gen-truss-protocast does not exist in $PATH")
	}

	protocOutDir, err := ioutil.TempDir("", "truss-")
	if err != nil {
		return nil, errors.Wrap(err, "cannot create temp directory")
	}
	defer os.RemoveAll(protocOutDir)

	pluginCall := "--truss-protocast_out=" + protocOutDir

	err = protoc(protoPaths, gopath, []string{pluginCall})
	if err != nil {
		return nil, errors.Wrap(err, "protoc failed")
	}

	fileInfo, err := ioutil.ReadDir(protocOutDir)
	if err != nil {
		return nil, errors.Wrapf(err, "cannot read directory: %v", protocOutDir)
	}

	for _, f := range fileInfo {
		if f.IsDir() {
			continue
		}
		fPath := filepath.Join(protocOutDir, f.Name())
		protocOut, err := ioutil.ReadFile(fPath)
		if err != nil {
			return nil, errors.Wrapf(err, "cannot read file: %v", fPath)
		}
		return protocOut, nil
	}

	return nil, errors.Errorf("no protoc output file found in: %v", protocOutDir)
}

// protoc executes protoc on protoPaths
func protoc(protoPaths, gopath []string, plugins []string) error {
	var cmdArgs []string

	cmdArgs = append(cmdArgs, "--proto_path="+filepath.Dir(protoPaths[0]))

	for _, gp := range gopath {
		cmdArgs = append(cmdArgs, "-I"+filepath.Join(gp, "src"))
		cmdArgs = append(cmdArgs, "-I"+gp)
	}

	cmdArgs = append(cmdArgs, plugins...)
	// Append each definition file path to the end of that command args
	cmdArgs = append(cmdArgs, protoPaths...)

	protocExec := exec.Command(
		"protoc",
		cmdArgs...,
	)

	outBytes, err := protocExec.CombinedOutput()
	if err != nil {
		return errors.Wrapf(err,
			"protoc exec failed.\nprotoc output:\n\n%v\nprotoc arguments:\n\n%v\n\n",
			string(outBytes), protocExec.Args)
	}

	return nil
}
