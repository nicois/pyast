package pyast

import (
	"testing"

	"github.com/alecthomas/repr"
)

func TestSimpleParsing(t *testing.T) {
	build := `
files(
    name="all-packages",
    sources=["all-packages.json"],
)


files(
    name="psqlrc",
    sources=["psqlrc", "psqlrc_prod"],
)

python_sources(overrides={"admplugin.py": {"dependencies": [":psqlrc"]}})`
	ast, err := parser.ParseString("", build)
	if err != nil {
		t.Fatal(err)
	}
	t.Log(repr.String(ast))
	t.Fatalf("%v\n", ast.Overrides())
}
