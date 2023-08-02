package main

// digraph main{
// 	edge[arrowhead=vee]
// 	graph [rankdir=LR,compound=true,ranksep=1.0];
// 	"github.com/Eun/goremovelines"[shape="record",label="goremovelines|github.com/Eun/goremovelines|goremovelines.go",style="solid"]
// 	"github.com/pkg/errors"[shape="record",label="errors|github.com/pkg/errors|errors.go\nstack.go",style="solid"]
// 	"github.com/pkg/errors2"[shape="record",label="errors|github.com/pkg/errors|errors.go\nstack.go",style="solid"]
// 	"github.com/Eun/goremovelines" -> "github.com/pkg/errors"[dir=forward]
// 	"github.com/Eun/goremovelines" -> "github.com/pkg/errors2"[dir=forward]
// }

import (
	"fmt"
	"go/parser"
	"go/token"
	"hash/crc32"
	"io"
	"os"
	"path/filepath"
	"strings"
	"unicode"

	"github.com/alecthomas/kingpin/v2"
)

var (
	version = ""
	commit  = ""
	date    = ""
)

var (
	sourceArg  = kingpin.Arg("src", "source directory").Default("./").ExistingDirs()
	outFlag    = kingpin.Flag("out", "output file").Default("-").String()
	formatFlag = kingpin.Flag("format", "format to use (dot or mermaidjs)").Default("dot").String()
	// recursiveFlag     = kingpin.Flag("recursive", "walk the sources recursive").Short('r').Default("true").Bool()
	includeTestFlag   = kingpin.Flag("tests", "include test files").Short('t').Default("false").Bool()
	fileLevelFlag     = kingpin.Flag("fl", "summarize on file level").Short('f').Default("false").Bool()
	includeGoRootFlag = kingpin.Flag("goroot", "include go root").Default("false").Bool()
	includeVendorFlag = kingpin.Flag("vendor", "include vendors directory").Default("false").Bool()
)

type dep struct {
	src string
	dst string
}

func (d *dep) Equal(a *dep) bool {
	return d.src == a.src && d.dst == a.dst
}

var gopath, goroot string

func main() {
	os.Exit(run())
}

func run() int {
	kingpin.Version(fmt.Sprintf("%s %s %s", version, commit, date))
	kingpin.Parse()

	if sourceArg == nil || len(*sourceArg) <= 0 {
		fmt.Fprintln(os.Stderr, "--src not defined or invalid")
		return 1
	}

	wd, err := os.Getwd()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Unable to get current directory: %s\n", err.Error())
		return 1
	}

	gopath = os.Getenv("GOPATH")
	if gopath == "" {
		fmt.Fprintln(os.Stderr, "GOPATH not defined")
		return 1
	}
	gopath = filepath.Join(gopath, "src")
	goroot = os.Getenv("GOROOT")
	if goroot == "" {
		fmt.Fprintln(os.Stderr, "GOROOT not defined")
		return 1
	}
	goroot = filepath.Join(goroot, "src")

	var out io.Writer
	if outFlag == nil || *outFlag == "" || *outFlag == "-" {
		out = os.Stdout
	} else {
		f, err := os.Create(*outFlag)
		if err != nil {
			fmt.Fprintf(os.Stderr, "unable to create file `%s': %s\n", *outFlag, err.Error())
			return 1
		}
		defer f.Close()
		out = f
	}

	var visited []string
	var deps []dep

	for i := 0; i < len(*sourceArg); i++ {
		path, err := filepath.Abs((*sourceArg)[i])
		if err != nil {
			fmt.Fprintf(os.Stderr, "unable to walk directory `%s': %s\n", (*sourceArg)[i], err.Error())
			continue
		}
		if err := walkFileSystem(&deps, &visited, path, true); err != nil {
			fmt.Fprintf(os.Stderr, "unable to walk directory `%s': %s\n", path, err.Error())
		}
	}

	for {
		unvisited := true
		for _, k := range deps {
			if !inVisited(k.dst, &visited) {
				unvisited = false
				if err := walkFileSystem(&deps, &visited, k.dst, false); err != nil {
					fmt.Fprintf(os.Stderr, "unable to walk directory `%s': %s\n", k.dst, err.Error())
				}
				break
			}
		}

		if unvisited {
			break
		}
	}

	// replace relative deps
	for i, dep := range deps {
		f := filepath.Base(dep.src)
		if filepath.Dir(dep.src) == "." {
			deps[i].src = filepath.Join(wd, f)
		}
	}

	// use package names instead of full paths

	for i, dep := range deps {
		deps[i].src = packageNameOfPath(dep.src)
		deps[i].dst = packageNameOfPath(dep.dst)
	}

	if !*fileLevelFlag {
		var depsDir []dep
		for i := 0; i < len(deps); i++ {
			depsDir = append(depsDir, dep{
				src: filepath.Dir(deps[i].src),
				dst: deps[i].dst,
			})
		}
		// remove duplicates
		removeDuplicateDeps(&depsDir)
		deps = make([]dep, len(depsDir))
		copy(deps, depsDir)
	}

	if formatFlag == nil {
		dotFormat := "dot"
		formatFlag = &dotFormat
	}

	if *formatFlag == "mermaidjs" {
		io.WriteString(out, "graph LR\n")
	} else {
		io.WriteString(out, "digraph main{\n\tedge[arrowhead=vee]\n\tgraph [rankdir=LR,compound=true,ranksep=1.0];\n")
	}

	for _, k := range deps {
		if *formatFlag == "mermaidjs" {
			fmt.Fprintf(out, "\t%d[%s] --> %d[%s]\n", crc32.ChecksumIEEE([]byte(k.src)), k.src, crc32.ChecksumIEEE([]byte(k.dst)), k.dst)
		} else {
			fmt.Fprintf(out, "\t\"%s\"[shape=\"record\",label=\"%s\",style=\"solid\"]\n", k.src, k.src)
			fmt.Fprintf(out, "\t\"%s\" -> \"%s\"\n", k.src, k.dst)
		}

	}

	if *formatFlag != "mermaidjs" {
		io.WriteString(out, "}")
	}

	return 0
}

func packageNameOfPath(p string) string {
	trimSlash := func(r rune) bool {
		return r == '/' || r == '\\'
	}
	if strings.HasPrefix(p, gopath) {
		return strings.TrimFunc(p[len(gopath):], trimSlash)
	}
	if strings.HasPrefix(p, goroot) {
		return strings.TrimFunc(p[len(goroot):], trimSlash)
	}
	return p
}

func removeDuplicateDeps(deps *[]dep) {
removeAgain:
	for i := len(*deps) - 1; i >= 0; i-- {
		removed := false
		for j := len(*deps) - 1; j >= 0; j-- {
			if i != j && (*deps)[i].Equal(&(*deps)[j]) {
				*deps = append((*deps)[:i], (*deps)[i+1:]...)
				removed = true
				break
			}
		}
		if removed {
			goto removeAgain
		}
	}
}

func walkFileSystem(deps *[]dep, visited *[]string, d string, root bool) error {
	d = filepath.Clean(d)
	if root == false {
		if d == "." || d == ".." {
			return nil
		}
	}
	if !*includeVendorFlag && strings.ToLower(d) == "vendor" {
		*visited = append(*visited, d)
		return nil
	}

	if inVisited(d, visited) {
		return nil
	}
	*visited = append(*visited, d)
	f, err := os.Open(d)
	if err != nil {
		return err
	}
	fi, err := f.Stat()
	if err != nil {
		f.Close()
		return err
	}

	if fi.IsDir() {
		fileInfos, err := f.Readdir(-1)
		if err != nil {
			f.Close()
			return nil
		}
		f.Close()

		for i := 0; i < len(fileInfos); i++ {
			if !*includeVendorFlag && strings.ToLower(fileInfos[i].Name()) == "vendor" {
				*visited = append(*visited, filepath.Join(d, fileInfos[i].Name()))
				continue
			}
			if err := walkFileSystem(deps, visited, filepath.Join(d, fileInfos[i].Name()), false); err != nil {
				fmt.Fprintf(os.Stderr, "unable to walk `%s': %s", fileInfos[i].Name(), err.Error())
			}
		}
		return nil
	} else if filepath.Ext(fi.Name()) == ".go" {
		if strings.HasSuffix(strings.ToLower(fi.Name()), "_test.go") {
			if !*includeTestFlag {
				return f.Close()
			}
		}

		defer f.Close()
		return walkFile(deps, visited, d, f)
	}
	return f.Close()
}

func walkFile(deps *[]dep, visited *[]string, path string, f *os.File) error {
	set := token.NewFileSet()
	astFile, err := parser.ParseFile(set, path, nil, parser.ImportsOnly)
	if err != nil {
		return fmt.Errorf("Failed to parse `%s': %s", path, err.Error())
	}

	for i := 0; i < len(astFile.Imports); i++ {
		imp := strings.TrimFunc(astFile.Imports[i].Path.Value, func(r rune) bool {
			return unicode.IsSpace(r) || r == '"'
		})

		fullImp := filepath.Join(gopath, imp)
		if dirExists(fullImp) {
			*deps = append(*deps, dep{
				src: path,
				dst: fullImp,
			})
			continue
		}
		if *includeGoRootFlag {
			fullImp = filepath.Join(goroot, imp)
			if dirExists(fullImp) {
				*deps = append(*deps, dep{
					src: path,
					dst: fullImp,
				})
				continue
			}
		}
		if *includeVendorFlag {
			fullImp = filepath.Join("vendor", imp)
			if dirExists(fullImp) {
				*deps = append(*deps, dep{
					src: path,
					dst: fullImp,
				})
				continue
			}
		}
	}
	return nil
}

func dirExists(p string) bool {
	f, err := os.Open(p)
	if err != nil {
		return false
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		return false
	}
	return fi.IsDir()
}

func inVisited(d string, visited *[]string) bool {
	for _, p := range *visited {
		if p == d {
			return true
		}
	}

	return false
}
