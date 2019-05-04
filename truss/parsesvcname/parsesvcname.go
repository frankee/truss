// Package parsesvcname will parse the service name of a protobuf package. The
// name returned will always be camelcased according to the conventions
// outlined in github.com/golang/protobuf/protoc-gen-go/generator.CamelCase.
package parsesvcname

import (
	"io"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"

	"github.com/frankee/truss/svcdef"
	"github.com/frankee/truss/truss/execprotoc"
	"github.com/pkg/errors"
)

func fileExists(path string) bool {
	if _, err := os.Stat(path); err == nil {
		return true
	}
	return false
}

func findFile(file string, includePath []string) (string, string) {
	for _, path := range includePath {
		fullPath := filepath.Join(path, file)
		if fileExists(fullPath) {
			return fullPath, path
		}
	}
	return "", ""
}

// FromPaths accepts the paths of a definition files and returns the
// name of the service in that protobuf definition file.
func FromPaths(includePaths []string, protoRawPaths []string, protoDefPaths []string) (*svcdef.Svcdef, map[string]*execprotoc.ProtoMetaInfo, error) {
	td, err := ioutil.TempDir("", "parsesvcname")
	defer os.RemoveAll(td)
	if err != nil {
		return nil, nil, errors.Wrap(err, "failed to create temporary directory for .pb.go files")
	}

	metaInfos := make(map[string]*execprotoc.ProtoMetaInfo)

	allImportFiles := make(map[string]bool)
	for idx, proto := range protoDefPaths {
		imports := execprotoc.GetProtoImports(proto)
		for _, i := range imports {
			if strings.HasPrefix(i, "google") || strings.HasPrefix(i, "github.com") {
				continue
			}

			allImportFiles[i] = true
		}

		metaInfo := execprotoc.GetProtoMetaInfo(proto)
		metaInfo.FilePath = protoRawPaths[idx]
		metaInfo.IncludePath = strings.TrimSuffix(proto, protoRawPaths[idx])
		metaInfos[protoRawPaths[idx]] = metaInfo
	}

	err = execprotoc.GeneratePBDotGo(protoRawPaths, includePaths, td)
	if err != nil {
		return nil, nil, errors.Wrap(err, "failed to generate .pb.go files from proto definition files")
	}

	for key := range allImportFiles {
		err = execprotoc.GeneratePBDotGo([]string{key}, includePaths, td)
		if err != nil {
			return nil, nil, errors.Wrap(err, "failed to generate .pb.go files from proto definition files")
		}

		fullPath, includePath := findFile(key, includePaths)
		metaInfo := execprotoc.GetProtoMetaInfo(fullPath)

		metaInfo.IncludePath = includePath
		metaInfo.FilePath = key

		metaInfos[key] = metaInfo
	}

	// Get path names of .pb.go files
	pbgoPaths := []string{}
	protoPaths := []string{}
	for _, meta := range metaInfos {
		base := filepath.Base(meta.FilePath)
		bareName := strings.TrimSuffix(base, filepath.Ext(meta.FilePath))
		pbgo := filepath.Join(td, meta.PackagePath, bareName+".pb.go")
		pbgoPaths = append(pbgoPaths, pbgo)

		protofile := filepath.Join(meta.IncludePath, meta.FilePath)
		protoPaths = append(protoPaths, protofile)
	}
	/*for _, p := range protoDefPaths {
		base := filepath.Base(p)
		barename := strings.TrimSuffix(base, filepath.Ext(p))
		pbgp := filepath.Join(td, barename+".pb.go")
		pbgoPaths = append(pbgoPaths, pbgp)
	}*/

	// Open all .pb.go files and store in map to be passed to svcdef.New()
	openFiles := func(paths []string) (map[string]io.Reader, error) {
		rv := map[string]io.Reader{}
		for _, p := range paths {
			reader, err := os.Open(p)
			if err != nil {
				return nil, errors.Wrapf(err, "cannot open file %q", p)
			}
			rv[p] = reader
		}
		return rv, nil
	}
	pbgoFiles, err := openFiles(pbgoPaths)
	if err != nil {
		return nil, nil, errors.Wrap(err, "cannot open all .pb.go files")
	}
	pbFiles, err := openFiles(protoPaths)
	if err != nil {
		return nil, nil, errors.Wrap(err, "cannot open all .proto files")
	}

	sd, err := svcdef.New(pbgoFiles, pbFiles)
	if err != nil {
		return nil, nil, errors.Wrapf(err, "failed to create service definition; did you pass ALL the protobuf files to truss?")
	}

	if sd.Service == nil {
		return nil, nil, errors.New("no service defined")
	}

	return sd, metaInfos, nil
}

func FromReaders(gopath []string, protoDefReaders []io.Reader) (string, error) {
	protoDir, err := ioutil.TempDir("", "parsesvcname-fromreaders")
	if err != nil {
		return "", errors.Wrap(err, "failed to create temporary directory for protobuf files")
	}
	// Ensures all the temporary files are removed
	defer os.RemoveAll(protoDir)

	protoDefPaths := []string{}
	for _, rdr := range protoDefReaders {
		f, err := ioutil.TempFile(protoDir, "parsesvcname-fromreader")
		_, err = io.Copy(f, rdr)
		if err != nil {
			return "", errors.Wrap(err, "couldn't copy contents of our proto file into the os.File: ")
		}
		path := f.Name()
		f.Close()
		protoDefPaths = append(protoDefPaths, path)
	}
	sd, _, err := FromPaths(gopath, protoDefPaths, protoDefPaths)
	if err != nil {
		return "", nil
	} else {
		return sd.Service.Name, nil
	}
}
