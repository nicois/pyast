package pyast

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"hash"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/getsentry/sentry-go"

	"github.com/nicois/cache"
	file "github.com/nicois/file"
	log "github.com/sirupsen/logrus"
	"golang.org/x/sync/semaphore"
)

type node struct {
	importers Classes
	isClass   bool // if true, it's a python class path. Otherwise, it's a absolute filesystem path
}
type tree struct {
	root  string
	nodes map[string]node // maps class
}

/*
bug:
tests/unit/deploy/test_sw_services.py
is not being detected as depending on
aiven/deploy/sw_services.py
*/

type trees []tree

/*
fixme: use a channel to collect the classes from goroutines
*/

type seen struct {
	nodes Classes
	mutex *sync.Mutex
}

func (s *seen) AddClass(class string) bool {
	// returns true if it was added (ie: not there already)
	s.mutex.Lock()
	defer s.mutex.Unlock()
	if _, ok := s.nodes[class]; ok {
		return false
	}
	s.nodes.Add(class)
	return true
}

func (t *tree) getClassDependencies(wg *sync.WaitGroup, s seen, class string, result chan string) {
	/*
	   If this class has dependencies, emit them out, and schedule recursive
	   checking unless they are already seen
	*/
	defer wg.Done()
	if node, ok := t.nodes[class]; ok {
		for dep := range node.importers {
			if s.AddClass(dep) {
				result <- dep
				wg.Add(1)
				go t.getClassDependencies(wg, s, dep, result)
			}
		}
	}
}

func (t *trees) GetDependees(paths file.Paths) (file.Paths, error) {
	// this is super fast; no need for goroutines
	result := file.CreatePaths()
	for _, tree := range *t {
		deps, err := tree.GetDependees(paths)
		if err != nil {
			return result, err
		}
		result.Union(deps)
	}
	return result, nil
}

func (t *tree) GetDependees(paths file.Paths) (file.Paths, error) {
	dependees := make(chan string, 10)
	result := file.CreatePaths()
	go func() {
		var wg sync.WaitGroup
		seen := seen{mutex: new(sync.Mutex), nodes: make(Classes)}
		for path := range paths {
			if p, err := filepath.Abs(path); err != nil || p != path {
				sentry.CaptureException(err)
				log.Fatalf("%v should be absolute already", path)
			}

			if !strings.HasPrefix(path, t.root) {
				log.Debugf("%v is not contained within %v. Ignoring it.", path, t.root)
				continue
			}
			if class, err := PathToClass(path[len(t.root)+1:]); err == nil {
				dependees <- class
				wg.Add(1)
				go t.getClassDependencies(&wg, seen, class, dependees)
			} else {
				log.Debugln(err)
			}
		}
		wg.Wait()
		close(dependees)
	}()
	for class := range dependees {
		if _, ok := result[class]; !ok {
			result.Add(ClassToPath(t.root, class))
		}
	}
	return result, nil
}

type depPair struct {
	importerClass string
	imported      string // ie: what is imported by the importer
	isClass       bool   // is the imported object a class? if not, assume it's an absolute path
}

func BuildTrees(pythonRoots file.Paths) *trees {
	var wg sync.WaitGroup
	c := make(chan tree)
	for pythonRoot := range pythonRoots {
		wg.Add(1)
		go BuildTree(&wg, c, pythonRoot)
	}
	go func() {
		wg.Wait()
		close(c)
	}()
	result := make(trees, 0)
	for t := range c {
		result = append(result, t)
	}
	if destinationFilename := os.Getenv("PYAST_DUMP_LOCATION"); destinationFilename != "" {
		destination, err := os.Create(destinationFilename)
		if err == nil {
			defer destination.Close()
			for _, tree := range result {
				destination.WriteString(fmt.Sprintf("Tree root: %v; %v nodes:\n", tree.root, len(tree.nodes)))
				for importee, node := range tree.nodes {
					destination.WriteString(fmt.Sprintf("\t%v is imported by: %v\n", importee, node.importers))
				}
				destination.WriteString(fmt.Sprintf("\n\n"))
			}
		} else {
			log.Warningf("Could not open %v so not creating a dump file: %v", destinationFilename, err)
		}

	}
	return &result
}

func BuildTree(pwg *sync.WaitGroup, c chan tree, pythonRoot string) {
	defer pwg.Done()
	pythonRoot, err := filepath.Abs(pythonRoot)
	if err != nil {
		sentry.CaptureException(err)
		log.Fatal(err)
	}
	var wg sync.WaitGroup
	depPairs := make(chan depPair)
	wg.Add(1)
	go buildDependencies(&wg, pythonRoot, depPairs)
	go func() {
		wg.Wait()
		close(depPairs)
	}()
	nodes := make(map[string]node)
	for pair := range depPairs {
		n, ok := nodes[pair.imported]
		if !ok {
			n = node{importers: CreateClasses(), isClass: pair.isClass}
			nodes[pair.imported] = n
		}
		n.importers.Add(pair.importerClass)
	}
	c <- tree{root: pythonRoot, nodes: nodes}
}

/*
this stuff is for a cache of the results, particuarly for pytestw,
as it will avoid having to even reload cache files from disk when just
a few files change. Disabled for now; concentrate on the disk-based cacher
first, as this will benefit shorts more.

type cache struct {
	ready       chan bool
	forwardRefs map[string]Classes
}

func createCache(pythonRoot string) cache {
	result := cache{ready: make(chan bool), forwardRefs: make(map[string]Classes)}
	defer close(result.ready)
	log.Debugf("todo: hash %v and open file if exists", pythonRoot)
	return result
}

func (c *cache) Get(path string) Classes {
	// wait for ready channel to be closed, if necessary
	if _, more := <-c.ready; more {
		log.Fatal("should not ever get anything sent to this channel")
	}
	if classes, ok := c.forwardRefs[path]; ok {
		return classes
	}
	return nil
}

func (c *cache) Put(path string, classes Classes) {
	// Can safely assume we Get() before Put()ting anything,
	// so no need to check the channel is closed
	c.forwardRefs[path] = classes
}
*/

func buildDependencies(wg *sync.WaitGroup, pythonRoot string, depPairs chan depPair) {
	// this controls the maximum number of files being read
	// concurrently, to avoid "too many open files" errors
	defer wg.Done()
	sem := semaphore.NewWeighted(10)
	pythonRoot, err := filepath.EvalSymlinks(pythonRoot)
	if err != nil {
		log.Infof("While evaluating symlink %v: %v. Temporarily ignoring this module.", pythonRoot, err)
		return
	}
	// create cache object w/ lock channel
	cacher, err := cache.Create[time.Time](context.Background(), "pyast")
	// FIXME: handle symlinks, either as files or directories
	filepath.WalkDir(pythonRoot, func(path string, d fs.DirEntry, err error) error {
		if d.IsDir() {
			if path != pythonRoot && !file.FileExists(filepath.Join(path, "__init__.py")) {
				// log.Debugf("%v does not contain __init__.py so skipping.", path)
				return fs.SkipDir
			}
			return nil
		}
		if strings.HasSuffix(path, ".py") {
			wg.Add(1)
			go scan(wg, cacher, depPairs, sem, pythonRoot, path)
		}
		if strings.HasSuffix(path, ".BUILD-wip") {
			wg.Add(1)
			go scanPants(wg, cacher, depPairs, sem, pythonRoot, path)
		}
		return nil
	})
}

func stripComments(source string) string {
	expressions := []*regexp.Regexp{regexp.MustCompile(`(?m)'''.+?'''`), regexp.MustCompile(`(?m)""".+?"""`)} //, regexp.MustCompile(`.+'''`)}
	for _, re := range expressions {
		source = re.ReplaceAllLiteralString(source, "")
	}
	re := regexp.MustCompile(`(?m)(\".*?\"|\'.*?\')|(#[^\r\n]*$)`)
	result := re.ReplaceAllStringFunc(source, func(s string) string {
		if strings.HasPrefix(s, "'") || strings.HasPrefix(s, "\"") {
			return s
		}
		return ""
	})
	return strings.TrimSpace(result)
}

func scanPants(wg *sync.WaitGroup, cacher cache.Cacher[time.Time], depPairs chan depPair, sem *semaphore.Weighted, root string, path string) {
	defer wg.Done()
	if !strings.HasPrefix(path, root) {
		log.Fatalf("%v does not start with %v, so something is wrong.", path, root)
	}
	sem.Acquire(context.Background(), 1)
	content, err := file.ReadBytes(path)
	sem.Release(1)
	if err != nil {
		sentry.CaptureException(err)
		log.Fatalf("While reading %v: %v", path, err)
	}
	pants, err := parser.ParseString(path, string(content))
	if err != nil {
		sentry.CaptureException(err)
		log.Fatal(err)
	}
	log.Debug(pants)
	// not going to cache this as it's probably just as fast to parse
	// depPairs <- depPair{importerClass: class, importedClass: dep, isClass: false}
	pants.SendDepPairs(depPairs)
}

func (p *Pants) SendDepPairs(depPairs chan depPair) {
	/*
	   python_tests: use overrides to get deps for specific test paths from "metapaths"
	*/
}

func scan(wg *sync.WaitGroup, cacher cache.Cacher[time.Time], depPairs chan depPair, sem *semaphore.Weighted, root string, path string) {
	/*
	   Responsible for sending depPairs on the designated channel for the specified python file at `path`.
	*/
	defer wg.Done()
	if !strings.HasPrefix(path, root) {
		log.Fatalf("%v does not start with %v, so cannot calculate the module path.", path, root)
	}
	ctx := context.Background()
	if err := sem.Acquire(ctx, 1); err != nil {
		return
	}
	content, err := file.ReadBytes(path)
	sem.Release(1)
	if err != nil {
		sentry.CaptureException(err)
		log.Fatalf("While reading %v: %v", path, err)
	}
	hasher := sha256.New()
	hasher.Write([]byte(path))
	class, err := PathToClass(path[len(root)+1:])
	if err != nil {
		log.Warnln(err)
	}
	versioner := cache.CreateReactiveListener(mtimeVersioner(path), time.Second/10)
	for dep := range createDependencies(ctx, cacher, hasher, versioner, class, content) {
		depPairs <- depPair{importerClass: class, imported: dep, isClass: true}
	}
}

func mtimeVersioner(path string) func() (time.Time, error) {
	return func() (time.Time, error) {
		fileinfo, err := os.Stat(path)
		if err != nil {
			return time.Time{}, err
		}
		return fileinfo.ModTime(), nil
	}
}

func extractImportsFromModule(class string, content string) Classes {
	reImport := regexp.MustCompile(`(?m)^\s*(?:from[ ]+(\S+)[ ]+)?import[ ]+([^\(]+?|\([^\)]+?\))(?:[ ]+as[ ]+\S+)?[ ]*$`)
	classes := CreateClasses()
	for _, match := range reImport.FindAllStringSubmatch(stripComments(string(content)), -1) {
		packageName := match[1]
		var names []string
		if strings.Contains(match[2], ",") {
			names = strings.Split(strings.Trim(match[2], "()"), ",")
		} else {
			names = []string{match[2]}
		}

		if strings.HasPrefix(packageName, ".") {
			// parentClass := class[:strings.LastIndex(class, ".")]
			parentClass := class
			for ; strings.HasPrefix(packageName, "."); packageName = packageName[1:] {
				if strings.LastIndex(class, ".") == -1 {
					log.Fatalf("Looking for . in %v (class) with %v (packageName) and %q (match)", class, packageName, match)
				}
				parentClass = parentClass[:strings.LastIndex(parentClass, ".")]
			}
			if len(packageName) > 1 {
				packageName = parentClass + "." + packageName
			} else {
				packageName = parentClass
			}
		}
		// add the parent as a dep, as e.g. "from foo import bar" might mean
		// foo is a module, or foo.bar
		for _, name := range names {
			var dep string
			if packageName == "" {
				dep = strings.TrimSpace(name)
			} else {
				dep = fmt.Sprintf("%v.%v", packageName, strings.TrimSpace(name))
			}
			classes.Add(dep)
			classes.Add(dep + ".__init__")
			if lastDotIndex := strings.LastIndex(dep, "."); lastDotIndex > 0 {
				classes.Add(dep[:lastDotIndex])
			}
		}
	}
	return classes
}

func createDependencies(ctx context.Context, cacher cache.Cacher[time.Time], hasher hash.Hash, versioner cache.Version[time.Time], class string, content []byte) Classes {
	serialisedClasses, err := cacher.Cache(ctx, hasher, func(ctx context.Context, stdout io.Writer, stderr io.Writer) ([]byte, error) {
		return json.Marshal(extractImportsFromModule(class, string(content)).Lister())
	}, versioner)
	if err != nil {
		sentry.CaptureException(err)
		log.Fatal(err)
	}
	if len(serialisedClasses) == 0 {
		log.Debug("No classes found.")
		return CreateClasses()
	}
	var listOfClasses []string
	if err = json.Unmarshal(serialisedClasses, &listOfClasses); err != nil {
		sentry.CaptureException(err)
		log.Fatalf("While processing %v: %v: %v", class, err, string(serialisedClasses))
	}
	result := CreateClasses(listOfClasses...)
	// log.Warnf("%v -> %q", class, listOfClasses)
	return result
}

func CalculatePythonRoots(paths file.Paths) file.Paths {
	result := file.CreatePaths()
	for path := range paths {
		if !strings.HasSuffix(path, ".py") {
			log.Debugf("%v does not appear to be a python module. Ignoring it.", path)
		}
		absolutePath, err := filepath.Abs(path)
		if err != nil {
			log.Info(err)
			continue
		}
		dir := filepath.Dir(absolutePath)
		for {
			if !file.FileExists(filepath.Join(dir, "__init__.py")) {
				break
			}
			dir = filepath.Dir(dir)
			if !file.DirExists(dir) {
				sentry.CaptureException(err)
				log.Fatalf("%v does not exist, while trying to find top-level of %v", dir, path)
			}
		}
		result.Add(dir)
	}
	return result
}

func PathToClass(path string) (string, error) {
	if !strings.HasSuffix(path, ".py") {
		return "", fmt.Errorf("'%v' is not a python file", path)
	}
	x := strings.ReplaceAll(path, "/", ".")
	class := x[:len(x)-3]
	// note: this leaves the __init__ suffix.
	// This is required to differentiate between
	// module and package paths.
	return class, nil
}

func ClassToPath(root string, class string) string {
	base := strings.ReplaceAll(class, ".", "/")
	if file.DirExists(base) {
		// PathToClass will retain the __init__ suffix,
		// but other sources may omit it. If so, put it back.
		return filepath.Join(root, base+"/__init__.py")
	}
	return filepath.Join(root, base+".py")
}
