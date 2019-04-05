package main

import (
	"bufio"
	"bytes"
	"fmt"
	"github.com/serenize/snaker"
	"go/build"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
	flag "github.com/spf13/pflag"

	"github.com/frankee/truss/truss"
	"github.com/frankee/truss/truss/execprotoc"
	"github.com/frankee/truss/truss/getstarted"
	"github.com/frankee/truss/truss/parsesvcname"

	ggkconf "github.com/frankee/truss/gengokit"
	gengokit "github.com/frankee/truss/gengokit/generator"
	"github.com/frankee/truss/svcdef"
)

var (
	svcPackageFlag = flag.String("svcout", "", "Go package path where the generated Go service will be written. Trailing slash will create a NAME-service directory")
	svcPathFlag = flag.String("svcpath", "", "svc path")
	includesFlag   = flag.StringArray("include", []string{}, "The proto files include paths")
	verboseFlag    = flag.BoolP("verbose", "v", false, "Verbose output")
	helpFlag       = flag.BoolP("help", "h", false, "Print usage")
	getStartedFlag = flag.BoolP("getstarted", "", false, "Output a 'getstarted.proto' protobuf file in ./")
)

var binName = filepath.Base(os.Args[0])
var workplace = ""

var (
	// Version is compiled into truss with the flag
	// go install -ldflags "-X main.Version=$SHA"
	Version string
	// BuildDate is compiled into truss with the flag
	// go install -ldflags "-X main.VersionDate=$VERSION_DATE"
	VersionDate string
)

func init() {
	ex, err := os.Executable()
	if err != nil {
		panic(err)
	}
	workplace = filepath.Dir(ex)
	fmt.Println(workplace)

	// If Version or VersionDate are not set, truss was not built with make
	if Version == "" || VersionDate == "" {
		//rebuild := promptNoMake()
		//if !rebuild {
		//	os.Exit(1)
		//}
		//err := makeAndRunTruss(os.Args)
		//exitIfError(errors.Wrap(err, "please install truss with make manually"))
		//os.Exit(0)
	}

	var buildinfo string
	buildinfo = fmt.Sprintf("version: %s", Version)
	buildinfo = fmt.Sprintf("%s version date: %s", buildinfo, VersionDate)

	flag.Usage = func() {
		if buildinfo != "" && (*verboseFlag || *helpFlag) {
			fmt.Fprintf(os.Stderr, "%s (%s)\n", binName, strings.TrimSpace(buildinfo))
		}
		fmt.Fprintf(os.Stderr, "\nUsage: %s [options] <protofile>...\n", binName)
		fmt.Fprintf(os.Stderr, "\nGenerates go-kit services using proto3 and gRPC definitions.\n")
		fmt.Fprintln(os.Stderr, "\nOptions:")
		flag.PrintDefaults()
	}
}

func main() {
	flag.Parse()

	if *helpFlag {
		flag.Usage()
		os.Exit(0)
	}

	log.SetLevel(log.InfoLevel)
	if *verboseFlag {
		log.SetLevel(log.DebugLevel)
	}

	if *getStartedFlag {
		pkg := ""
		if len(flag.Args()) > 0 {
			pkg = flag.Args()[0]
		}
		os.Exit(getstarted.Do(pkg))
	}

	if len(flag.Args()) == 0 {
		fmt.Fprintf(os.Stderr, "%s: missing .proto file(s)\n", binName)
		flag.Usage()
		os.Exit(1)
	}

	cfg, err := parseInput()
	exitIfError(errors.Wrap(err, "cannot parse input"))

	// If there was no service found in parseInput, the rest can be omitted.
	if cfg == nil {
		return
	}

	//sd, err := parseServiceDefinition(cfg)
	//exitIfError(errors.Wrap(err, "cannot parse input definition proto files"))

	genFiles, err := generateCode(cfg, cfg.SvcDef)
	exitIfError(errors.Wrap(err, "cannot generate service"))

	for path, file := range genFiles {
		err := writeGenFile(file, filepath.Join(cfg.ServicePath, path))
		if err != nil {
			exitIfError(errors.Wrap(err, "cannot to write output"))
		}
	}
}

// parseInput constructs a *truss.Config with all values needed to parse
// service definition files.
func parseInput() (*truss.Config, error) {
	var cfg truss.Config

	// GOPATH
	cfg.GoPath = filepath.SplitList(os.Getenv("GOPATH"))
	if len(cfg.GoPath) == 0 {
		return nil, errors.New("GOPATH not set")
	}
	log.WithField("GOPATH", cfg.GoPath).Debug()

	// IncludePath
	if len(*includesFlag) > 0 {
		cfg.IncludePaths = *includesFlag
	}
	log.WithField("IncludePaths", cfg.IncludePaths).Debug()

	if len(*svcPathFlag) > 0 {
		cfg.SvcPath = *svcPathFlag
	}
	log.WithField("SvcPath", cfg.IncludePaths).Debug()

	// DefPaths
	var err error
	cfg.RawPaths = flag.Args()
	cfg.DefPaths, err = cleanProtofilePath(cfg.RawPaths, cfg.IncludePaths)
	if err != nil {
		return nil, errors.Wrap(err, "cannot parse input arguments")
	}
	log.WithField("DefPaths", cfg.DefPaths).Debug()

	//protoDir := filepath.Dir(cfg.DefPaths[0])
	//p, err := build.Default.ImportDir(protoDir, build.FindOnly)
	//if err != nil {
	//	return nil, err
	//}
	//if p.Root == "" {
	//	return nil, errors.New("proto files not in GOPATH")
	//}

	includePaths := cfg.GoPath
	includePaths = append(includePaths, cfg.IncludePaths...)
	if err := execprotoc.GeneratePBDotGo(cfg.RawPaths, includePaths, cfg.PBGoPath); err != nil {
		return nil, errors.Wrap(err, "cannot create .pb.go files")
	}

	// Service Path
	sd, metaInfos, err := parsesvcname.FromPaths(cfg.IncludePaths, cfg.RawPaths, cfg.DefPaths)
	if err != nil {
		log.Warn("No valid service is defined; exiting now.", err.Error())
		log.Info(".pb.go generation with protoc-gen-go was successful.")
		return nil, nil
	}
	cfg.SvcDef = sd
	cfg.MetaInfos = metaInfos

	serviceMeta := metaInfos[cfg.RawPaths[0]]
	cfg.PBGoPackage = serviceMeta.PackagePath
	cfg.PBGoPath = serviceMeta.PackageName
	log.WithField("PB Package", cfg.PBGoPackage).Debug()
	log.WithField("PB Path", cfg.PBGoPath).Debug()

	sd.PbPkgName = filepath.Dir(serviceMeta.FilePath)
	sd.PbPkgName = strings.Replace(sd.PbPkgName, "/", ".", -1)
	svcName := sd.Service.Name
	svcName = snaker.CamelToSnake(svcName)
	svcName = strings.Replace(svcName, "_", "-", -1)

	svcDirName := svcName + "-service"
	log.WithField("svcDirName", svcDirName).Debug()

	svcPath := filepath.Join(filepath.Dir(cfg.DefPaths[0]), svcDirName)
	//svcPath := filepath.Join(workplace, svcDirName)

	if *svcPackageFlag != "" {
		svcOut := *svcPackageFlag
		log.WithField("svcPackageFlag", svcOut).Debug()

		// If the package flag ends in a seperator, file will be "".
		_, file := filepath.Split(svcOut)
		seperator := file == ""
		log.WithField("seperator", seperator)

		svcPath, err = parseSVCOut(svcOut, "")
		if err != nil {
			return nil, errors.Wrapf(err, "cannot parse svcout: %s", svcOut)
		}

		// Join the svcDirName as a svcout ending with `/` should create it
		if seperator {
			svcPath = filepath.Join(svcPath, svcDirName)
		}
	}

	log.WithField("svcPath", svcPath).Debug()

	// Create svcPath for the case that it does not exist
	err = os.MkdirAll(svcPath, 0777)
	if err != nil {
		return nil, errors.Wrapf(err, "cannot create svcPath directory: %s", svcPath)
	}

	p, err := build.Default.ImportDir(svcPath, build.FindOnly)
	if err != nil {
		log.WithError(err).Error()
		return nil, err
	}
	//if p.Root == "" {
	//	return nil, errors.New("proto files path not in GOPATH")
	//}

	cfg.ServicePackage = filepath.Join(cfg.SvcPath, svcName + "-service")
	cfg.ServicePath = p.Dir

	log.WithField("Service Package", cfg.ServicePackage).Debug()
	log.WithField("Service Path", cfg.ServicePath).Debug()

	// PrevGen
	cfg.PrevGen, err = readPreviousGeneration(cfg.ServicePath)
	if err != nil {
		return nil, errors.Wrap(err, "cannot read previously generated files")
	}

	return &cfg, nil
}

// parseSVCOut handles the difference between relative paths and go package
// paths
func parseSVCOut(svcOut string, GOPATH string) (string, error) {
	if build.IsLocalImport(svcOut) {
		return filepath.Abs(svcOut)
	}

	if len(GOPATH) > 0 {
		return filepath.Join(GOPATH, "src", svcOut), nil
	} else {
		return svcOut, nil
	}
}

// parseServiceDefinition returns a svcdef which contains all necessary
// information for generating a truss service.
func parseServiceDefinition(cfg *truss.Config) (*svcdef.Svcdef, error) {
	protoDefPaths := cfg.DefPaths
	// Create the ServicePath so the .pb.go files may be place within it
	if cfg.PrevGen == nil {
		err := os.MkdirAll(cfg.ServicePath, 0777)
		if err != nil {
			return nil, errors.Wrap(err, "cannot create service directory")
		}
	}

	// Get path names of .pb.go files
	pbgoPaths := []string{}
	for _, p := range protoDefPaths {
		base := filepath.Base(p)
		barename := strings.TrimSuffix(base, filepath.Ext(p))
		pbgp := filepath.Join(cfg.PBGoPath, barename+".pb.go")
		pbgoPaths = append(pbgoPaths, pbgp)
	}
	pbgoFiles, err := openFiles(pbgoPaths)
	if err != nil {
		return nil, errors.Wrap(err, "cannot open all .pb.go files")
	}

	pbFiles, err := openFiles(protoDefPaths)
	if err != nil {
		return nil, errors.Wrap(err, "cannot open all .proto files")
	}

	// Create the svcdef
	sd, err := svcdef.New(pbgoFiles, pbFiles)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to create service definition; did you pass ALL the protobuf files to truss?")
	}

	// TODO: Remove once golang 1.9 comes out and type aliases solve context vs golang.org/x/net/context
	if err := rewritePBGoForContext(sd.Service.Name, pbgoPaths); err != nil {
		return nil, errors.Wrap(err, "cannot rewrite .pb.go files")
	}

	return sd, nil
}

// TODO: Remove once golang 1.9 comes out and type aliases solve context vs golang.org/x/net/context
func rewritePBGoForContext(serviceName string, pbgoPaths []string) error {
	pbgoFiles, err := openFiles(pbgoPaths)
	if err != nil {
		return errors.Wrap(err, "cannot open all .pb.go files")
	}

	const oldContextImport = `context "golang.org/x/net/context"`
	const newContextImport = `newcontext "context"`
	serverInterface := serviceName + "Server"

	for path, f := range pbgoFiles {
		newPBGoFile := bytes.NewBuffer(nil)
		s := bufio.NewScanner(f)
		var readingServerInterface bool

		for s.Scan() {
			line := s.Text()

			// Add the `newcontext "context"` import if we are on the context
			// import line
			if strings.Contains(line, oldContextImport) {
				line = line + "\n\t" + newContextImport
			}

			// If we are not reading the service interface check if we need to start
			if !readingServerInterface {
				// Found the start of the {{.Service.Name}}Server interface
				if strings.HasPrefix(line, "type "+serverInterface+" interface {") {
					readingServerInterface = true
				}
			}

			// If we are reading the {{.Service.Name}}Server interface
			if readingServerInterface {
				// Replace `(context` with `(newcontext`
				line = strings.Replace(line, "(context", "(newcontext", 1)

				// Reached the end of the {{.Service.Name}}Server interface
				if strings.HasPrefix(line, "}") {
					readingServerInterface = false
				}
			}

			// Write the line to the new file buffer
			_, err := newPBGoFile.WriteString(line + "\n")
			if err != nil {
				return errors.Wrap(err, "cannot write to new .pb.go file")
			}
		}

		// Write the rewritten .pb.go file
		err = writeGenFile(newPBGoFile, path)
		if err != nil {
			return errors.Wrap(err, "cannot write new .pb.go file to disk")
		}
	}

	return nil
}

// generateCode returns a map[string]io.Reader that represents a gokit
// service
func generateCode(cfg *truss.Config, sd *svcdef.Svcdef) (map[string]io.Reader, error) {
	conf := ggkconf.Config{
		PBGoPackage:     cfg.PBGoPackage,
		GoPackage:     cfg.ServicePackage,
		PreviousFiles: cfg.PrevGen,
		Version:       Version,
		VersionDate:   VersionDate,
	}

	genGokitFiles, err := gengokit.GenerateGokit(sd, conf)
	if err != nil {
		return nil, errors.Wrap(err, "cannot generate gokit service")
	}

	return genGokitFiles, nil
}

func openFiles(paths []string) (map[string]io.Reader, error) {
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

// writeGenFile writes a file at path to the filesystem
func writeGenFile(file io.Reader, path string) error {
	err := os.MkdirAll(filepath.Dir(path), 0777)
	if err != nil {
		return err
	}

	outFile, err := os.Create(path)
	if err != nil {
		return errors.Wrapf(err, "cannot create file %v", path)
	}

	_, err = io.Copy(outFile, file)
	if err != nil {
		return errors.Wrapf(err, "cannot write to %v", path)
	}
	return outFile.Close()
}

// cleanProtofilePath returns the absolute filepath of a group of files
// of the files, or an error if the files are not in the same directory
func cleanProtofilePath(rawPaths []string, includePaths []string) ([]string, error) {
	var fullPaths []string

	// Parsed passed file paths
	for _, def := range rawPaths {
		log.WithField("rawDefPath", def).Debug()
		full, err := filepath.Abs(def)
		if err != nil {
			return nil, errors.Wrap(err, "cannot get working directory of truss")
		}

		if !fileExists(full) {
			for _, path := range includePaths {
				fullPath := filepath.Join(path, def)
				if fileExists(fullPath) {
					full = fullPath
					break
				}
			}
		}
		log.WithField("fullDefPath", full)

		if !fileExists(full) {
			return nil, errors.Wrap(err, "file not exist")
		}

		fullPaths = append(fullPaths, full)
		if filepath.Dir(fullPaths[0]) != filepath.Dir(full) {
			return nil, errors.Errorf("passed .proto files in different directories")
		}
	}

	return fullPaths, nil
}

// exitIfError will print the error message and exit 1 if the passed error is
// non-nil
func exitIfError(err error) {
	if errors.Cause(err) != nil {
		defer os.Exit(1)
		if *verboseFlag {
			fmt.Printf("%+v\n", err)
			return
		}
		fmt.Printf("%v\n", err)
	}
}

// readPreviousGeneration returns a map[string]io.Reader representing the files in serviceDir
func readPreviousGeneration(serviceDir string) (map[string]io.Reader, error) {
	if !fileExists(serviceDir) {
		return nil, nil
	}

	files := make(map[string]io.Reader)

	addFileToFiles := func(path string, info os.FileInfo, err error) error {
		if info.IsDir() {
			return nil
		}

		file, ioErr := os.Open(path)

		if ioErr != nil {
			return errors.Wrapf(ioErr, "cannot read file: %v", path)
		}

		// trim the prefix of the path to the proto files from the full path to the file
		relPath, err := filepath.Rel(serviceDir, path)
		if err != nil {
			return err
		}

		// ensure relPath is unix-style, so it matches what we look for later
		relPath = filepath.ToSlash(relPath)

		files[relPath] = file

		return nil
	}

	err := filepath.Walk(serviceDir, addFileToFiles)
	if err != nil {
		return nil, errors.Wrapf(err, "cannot fully walk directory %v", serviceDir)
	}

	return files, nil
}

// fileExists checks if a file at the given path exists. Returns true if the
// file exists, and false if the file does not exist.
func fileExists(path string) bool {
	if _, err := os.Stat(path); err == nil {
		return true
	}
	return false
}

// promptNoMake prints that truss was not built with make and prompts the user
// asking if they would like for this process to be automated
// returns true if yes, false if not.
func promptNoMake() bool {
	const msg = `
truss was not built using Makefile.
Please run 'make' inside go import path %s.

Do you want to automatically run 'make' and rerun command:

	$ `
	fmt.Printf(msg, trussImportPath)
	for _, a := range os.Args {
		fmt.Print(a)
		fmt.Print(" ")
	}
	const q = `

? [Y/n] `
	fmt.Print(q)

	var response string
	_, err := fmt.Scanln(&response)
	if err != nil {
		exitIfError(err)
	}

	switch strings.ToLower(strings.TrimSpace(response)) {
	case "y", "yes":
		return true
	}
	return false
}

const trussImportPath = "github.com/frankee/truss"

// makeAndRunTruss installs truss by running make in trussImportPath.
// It then passes through args to newly installed truss.
func makeAndRunTruss(args []string) error {
	p, err := build.Default.Import(trussImportPath, "", build.FindOnly)
	if err != nil {
		return errors.Wrap(err, "could not find truss directory")
	}
	make := exec.Command("make")
	make.Dir = p.Dir
	err = make.Run()
	if err != nil {
		return errors.Wrap(err, "could not run make in truss directory")
	}
	truss := exec.Command("truss", args[1:]...)
	truss.Stdin, truss.Stdout, truss.Stderr = os.Stdin, os.Stdout, os.Stderr
	return truss.Run()
}
