package pyast

import (
	"context"
	"path/filepath"
	"testing"

	file "github.com/nicois/file"
)

// TestHelloName calls greetings.Hello with a name, checking
// for a valid return value.
func TestStripComments(t *testing.T) {
	// these aren't actually comments but for our purposes
	// can be discarded
	if stripped := stripComments(`import foo ''' or not'''`); stripped != `import foo` {
		t.Fatalf("Stripped version was:\n%v\n, which is wrong", stripped)
	}

	if stripped := stripComments(`import foo """ or not"""`); stripped != `import foo` {
		t.Fatalf("Stripped version was:\n%v\n, which is wrong", stripped)
	}

	// single-line comment
	if stripped := stripComments(`import foo # or not`); stripped != `import foo` {
		t.Fatalf("Stripped version was:\n%q\n, which is wrong", stripped)
	}

	// single-line comment, multiple imports
	if stripped := stripComments(`from aiven.deploy.sw_services import DeploySWService, DeploySWServiceFileLookupRule, SwServices`); stripped != `from aiven.deploy.sw_services import DeploySWService, DeploySWServiceFileLookupRule, SwServices` {
		t.Fatalf("Stripped version was:\n%q\n, which is wrong", stripped)
	}

	// multi-line import with comments
	if stripped := stripComments(`
from foo import (# or not
    bar,# another comment
    baz)
    `); stripped != `from foo import (
    bar,
    baz)` {
		t.Fatalf("Stripped version was:\n%q\n, which is wrong", stripped)
	}
}

func TestExtractImportsFromModule(t *testing.T) {
	if actual := extractImportsFromModule("", `
import foo
    `); !CreateClasses("foo", "foo.__init__").SameAs(actual) {
		t.Fatalf("Found unexpected result:\n%q", actual)
	}

	if actual := extractImportsFromModule("", `
import foo
import bar
`); !CreateClasses("foo", "foo.__init__", "bar", "bar.__init__").SameAs(actual) {
		t.Fatalf("Found unexpected result:\n%q", actual)
	}

	if actual := extractImportsFromModule("myapp", `
import foo, bar,baz
    `); !CreateClasses("foo", "bar", "baz", "foo.__init__", "bar.__init__", "baz.__init__").SameAs(actual) {
		t.Fatalf("Found unexpected result:\n%q", actual)
	}

	if actual := extractImportsFromModule("", `
from foo import baz
    `); !CreateClasses("foo.baz", "foo", "foo.baz.__init__").SameAs(actual) {
		t.Fatalf("Found unexpected result:\n%q", actual)
	}

	// 'foo' could be the module with the 4 items defined in it,
	// or 'foo' could be a package and 'bar' etc are modules or subpackages.
	if actual := extractImportsFromModule("", `
from foo import (bar,
    baz,
    biffo, bert)
    `); !CreateClasses("foo.bar", "foo.baz", "foo.biffo", "foo.bert", "foo", "foo.bar.__init__", "foo.baz.__init__", "foo.biffo.__init__", "foo.bert.__init__").SameAs(actual) {
		t.Fatalf("Found unexpected result:\n%q", actual)
	}

	if actual := extractImportsFromModule("myapp.__init__", `
from .sibling import foo, bar,baz
    `); !CreateClasses("myapp.sibling.foo", "myapp.sibling.bar", "myapp.sibling.baz",
		"myapp.sibling.foo.__init__", "myapp.sibling.bar.__init__", "myapp.sibling.baz.__init__",
		"myapp.sibling").SameAs(actual) {
		t.Fatalf("Found unexpected result:\n%q", actual)
	}

	if actual := extractImportsFromModule("myapp.utils.__init__", `
from ..aunt import foo
    `); !CreateClasses("myapp.aunt.foo", "myapp.aunt.foo.__init__", "myapp.aunt").SameAs(actual) {
		t.Fatalf("Found unexpected result:\n%q", actual)
	}

	if actual := extractImportsFromModule("myapp.utils.__init__", `
from myapp.deploy.services import foo, bar
    `); !CreateClasses("myapp.deploy.services.foo", "myapp.deploy.services.foo.__init__", "myapp.deploy.services", "myapp.deploy.services.bar", "myapp.deploy.services.bar.__init__").SameAs(actual) {
		t.Fatalf("Found unexpected result:\n%q", actual)
	}
}

func TestBuildTreesNamespacePackages(t *testing.T) {
	root, err := filepath.Abs("testdata/namespace/src")
	if err != nil {
		t.Fatal(err)
	}
	roots := file.CreatePaths(root)
	ctx := context.Background()

	opts := BuildTreesOptions{NamespacePackages: true}
	trees := BuildTreesWithOptions(ctx, roots, opts)

	consumerPath := filepath.Join(root, "avn/kafka/consumer.py")
	deps, err := trees.GetDependees(file.CreatePaths(consumerPath))
	if err != nil {
		t.Fatal(err)
	}

	producerPath := filepath.Join(root, "avn/kafka/producer.py")
	if _, ok := deps[producerPath]; !ok {
		t.Errorf("expected producer.py to depend on consumer.py, got: %v", deps)
	}
}

func TestCrossRootDependees(t *testing.T) {
	repoRoot, _ := filepath.Abs("testdata/crossroot/repo")
	kafkaSrc, _ := filepath.Abs("testdata/crossroot/repo/py/kafka/src")

	roots := file.CreatePaths(repoRoot, kafkaSrc)
	opts := BuildTreesOptions{NamespacePackages: true}
	trees := BuildTreesWithOptions(context.Background(), roots, opts)

	consumerPath := filepath.Join(kafkaSrc, "avn/kafka/consumer.py")
	deps, err := trees.GetDependees(file.CreatePaths(consumerPath))
	if err != nil {
		t.Fatal(err)
	}

	apiPath := filepath.Join(repoRoot, "aiven/acorn/api.py")
	if _, ok := deps[apiPath]; !ok {
		t.Errorf("expected aiven/acorn/api.py to depend on consumer.py via cross-root import\ngot: %v", deps)
	}
}

func TestOverlappingRootPrefixes(t *testing.T) {
	repoRoot, _ := filepath.Abs("testdata/crossroot/repo")
	kafkaSrc, _ := filepath.Abs("testdata/crossroot/repo/py/kafka/src")

	roots := file.CreatePaths(repoRoot, kafkaSrc)
	opts := BuildTreesOptions{NamespacePackages: true}
	trees := BuildTreesWithOptions(context.Background(), roots, opts)

	consumerPath := filepath.Join(kafkaSrc, "avn/kafka/consumer.py")
	class, ok := trees.pathToClassAcrossTrees(consumerPath)
	if !ok {
		t.Fatal("expected to find class for consumer.py")
	}
	if class != "avn.kafka.consumer" {
		t.Errorf("expected class avn.kafka.consumer, got %s", class)
	}
}

func TestCrossProjectAvnImports(t *testing.T) {
	repoRoot, _ := filepath.Abs("testdata/crossroot/repo")
	kafkaSrc, _ := filepath.Abs("testdata/crossroot/repo/py/kafka/src")
	metricsSrc, _ := filepath.Abs("testdata/crossroot/repo/py/metrics/src")

	roots := file.CreatePaths(repoRoot, kafkaSrc, metricsSrc)
	opts := BuildTreesOptions{NamespacePackages: true}
	trees := BuildTreesWithOptions(context.Background(), roots, opts)

	consumerPath := filepath.Join(kafkaSrc, "avn/kafka/consumer.py")
	deps, err := trees.GetDependees(file.CreatePaths(consumerPath))
	if err != nil {
		t.Fatal(err)
	}

	collectorPath := filepath.Join(metricsSrc, "avn/metrics/collector.py")
	if _, ok := deps[collectorPath]; !ok {
		t.Errorf("expected avn/metrics/collector.py to depend on avn/kafka/consumer.py\ngot: %v", deps)
	}
}

func TestTransitiveCrossRootDeps(t *testing.T) {
	repoRoot, _ := filepath.Abs("testdata/crossroot/repo")
	kafkaSrc, _ := filepath.Abs("testdata/crossroot/repo/py/kafka/src")
	metricsSrc, _ := filepath.Abs("testdata/crossroot/repo/py/metrics/src")

	roots := file.CreatePaths(repoRoot, kafkaSrc, metricsSrc)
	opts := BuildTreesOptions{NamespacePackages: true}
	trees := BuildTreesWithOptions(context.Background(), roots, opts)

	consumerPath := filepath.Join(kafkaSrc, "avn/kafka/consumer.py")
	deps, err := trees.GetDependees(file.CreatePaths(consumerPath))
	if err != nil {
		t.Fatal(err)
	}

	apiPath := filepath.Join(repoRoot, "aiven/acorn/api.py")
	collectorPath := filepath.Join(metricsSrc, "avn/metrics/collector.py")
	if _, ok := deps[apiPath]; !ok {
		t.Errorf("expected aiven/acorn/api.py in dependees, got: %v", deps)
	}
	if _, ok := deps[collectorPath]; !ok {
		t.Errorf("expected avn/metrics/collector.py in dependees, got: %v", deps)
	}
}

func TestBuildTreesBackwardCompat(t *testing.T) {
	root, err := filepath.Abs("testdata/namespace/src")
	if err != nil {
		t.Fatal(err)
	}
	roots := file.CreatePaths(root)
	ctx := context.Background()

	// Without namespace packages, the avn/ directory (no __init__.py) should be skipped
	trees := BuildTrees(ctx, roots)

	consumerPath := filepath.Join(root, "avn/kafka/consumer.py")
	deps, err := trees.GetDependees(file.CreatePaths(consumerPath))
	if err != nil {
		t.Fatal(err)
	}

	producerPath := filepath.Join(root, "avn/kafka/producer.py")
	if _, ok := deps[producerPath]; ok {
		t.Errorf("without namespace packages, producer.py should NOT be detected as depending on consumer.py, but got: %v", deps)
	}
}
