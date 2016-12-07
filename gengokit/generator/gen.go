// Package generator generates a gokit service based on a service definition.
package generator

import (
	"bytes"
	"go/format"
	"io"
	"io/ioutil"
	"strings"

	log "github.com/Sirupsen/logrus"
	"github.com/pkg/errors"

	"github.com/TuneLab/go-truss/gengokit"
	"github.com/TuneLab/go-truss/gengokit/handler"
	"github.com/TuneLab/go-truss/gengokit/middlewares"
	templFiles "github.com/TuneLab/go-truss/gengokit/template"

	"github.com/TuneLab/go-truss/svcdef"
)

// GenerateGokit returns a gokit service generated from a service definition (svcdef),
// the package to the root of the generated service goPackage, the package
// to the .pb.go service struct files (goPBPackage) and any prevously generated files.
func GenerateGokit(sd *svcdef.Svcdef, conf gengokit.Config) (map[string]io.Reader, error) {
	data, err := gengokit.NewData(sd, conf)
	if err != nil {
		return nil, errors.Wrap(err, "cannot create template data")
	}

	codeGenFiles := make(map[string]io.Reader)

	for _, templPath := range templFiles.AssetNames() {
		actualPath := templatePathToActual(templPath, sd.PkgName)
		file, err := generateResponseFile(templPath, data, conf.PreviousFiles[actualPath])
		if err != nil {
			return nil, errors.Wrap(err, "cannot render template")
		}

		codeGenFiles[actualPath] = file
	}

	return codeGenFiles, nil
}

// generateResponseFile contains logic to choose how to render a template file
// based on path and if that file was generated previously. It accepts a
// template path to render, a templateExecutor to apply to the template, and a
// map of paths to files for the previous generation. It returns a
// io.Reader representing the generated file.
func generateResponseFile(templFP string, data *gengokit.Data, prevFile io.Reader) (io.Reader, error) {
	var genCode io.Reader
	var err error

	// Get the actual path to the file rather than the template file path
	actualFP := templatePathToActual(templFP, data.PackageName)

	switch templFP {
	case handler.ServerHandlerPath:
		h, err := handler.New(data.Service, prevFile, data.PackageName)
		if err != nil {
			return nil, errors.Wrapf(err, "cannot parse previous handler: %q", actualFP)
		}

		if genCode, err = h.Render(templFP, data); err != nil {
			return nil, errors.Wrapf(err, "cannot render template: %s", templFP)
		}
	case middlewares.EndpointsPath:
		m := middlewares.New()
		m.LoadEndpoints(prevFile)
		if genCode, err = m.Render(templFP, data); err != nil {
			return nil, errors.Wrapf(err, "cannot render template: %s", templFP)
		}
	case middlewares.ServicePath:
		m := middlewares.New()
		m.LoadService(prevFile)
		if genCode, err = m.Render(templFP, data); err != nil {
			return nil, errors.Wrapf(err, "cannot render template: %s", templFP)
		}
	default:
		if genCode, err = applyTemplateFromPath(templFP, data); err != nil {
			return nil, errors.Wrapf(err, "cannot render template: %s", templFP)
		}
	}

	codeBytes, err := ioutil.ReadAll(genCode)
	if err != nil {
		return nil, err
	}

	// ignore error as we want to write the code either way to inspect after
	// writing to disk
	formattedCode := formatCode(codeBytes)

	return bytes.NewReader(formattedCode), nil
}

// templatePathToActual accepts a templateFilePath and the packageName of the
// service and returns what the relative file path of what should be written to
// disk
func templatePathToActual(templFilePath, packageName string) string {
	// Switch "NAME" in path with packageName.
	// i.e. for packageName = addsvc; /NAME-service/NAME-server -> /addsvc-service/addsvc-server
	actual := strings.Replace(templFilePath, "NAME", packageName, -1)

	actual = strings.TrimSuffix(actual, "template")

	return actual
}

// applyTemplateFromPath calls applyTemplate with the template at templFilePath
func applyTemplateFromPath(templFP string, data *gengokit.Data) (io.Reader, error) {
	templBytes, err := templFiles.Asset(templFP)
	if err != nil {
		return nil, errors.Wrapf(err, "unable to find template file: %v", templFP)
	}

	return data.ApplyTemplate(string(templBytes), templFP)
}

// formatCode takes a string representing golang code and attempts to return a
// formated copy of that code.  If formatting fails, a warning is logged and
// the original code is returned.
func formatCode(code []byte) []byte {
	formatted, err := format.Source(code)

	if err != nil {
		log.WithError(err).Warn("Code formatting error, generated service will not build, outputting unformatted code")
		// return code so at least we get something to examine
		return code
	}

	return formatted
}