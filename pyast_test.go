package pyast

import (
	"testing"
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
