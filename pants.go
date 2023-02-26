package pyast

import (
    "fmt"
	log "github.com/sirupsen/logrus"
	file "github.com/nicois/file"
	"github.com/alecthomas/participle/v2"
	"github.com/alecthomas/participle/v2/lexer"
)

type Pants struct {
	Directives []*Directive `@@*`
}

func (p *Pants) Overrides() []string {
    result := []string{}
    if p.Directives == nil {
        return result
    }
    for _, d := range p.Directives {
        if d.Name == "python_sources" {
            for _, a := range d.Args {
                if a.Name == "overrides" {
                    for _, o := range a.Value.Dict.DictElements {
                    result = append(result, o.Name)
                }
            }
        }
    }
}
    return result
}
var graphQLLexer = lexer.MustSimple([]lexer.SimpleRule{
		{"Comment", `(?:#)[^\n]*\n?`},
        {"Ident", `[a-zA-Z\-_0-9]+`},
        {"Punctuation", `[()=,\[\]:{}]`},
        {"Text", `"[^"]*"`},
		{"Whitespace", `[ \t\n\r]+`},
	})
var parser = participle.MustBuild[Pants](
		participle.Lexer(graphQLLexer),
		participle.Elide("Comment", "Whitespace"),
        participle.Unquote("Text"),
		participle.UseLookahead(2),
	)
func ParsePants(path string) (*Pants, error) {
	b, err := file.ReadBytes(path)
	if err != nil {
        return nil, err
	}
	p, err := parser.ParseString(path, string(b))
	if err != nil {
        l, lerr :=graphQLLexer.LexString(path, string(b))
        if lerr != nil {
            return nil, lerr
        }
        for {
            next, nerr := l.Next()
            if nerr != nil {
                return nil, nerr
            }
            if next.EOF() {
                break
            }
            log.Info(next.GoString())
        }


        return nil, err
	}
    return p, nil
}


type Directive struct {
    Name string `@Ident`
    Args []*Arg `( "(" ( @@ ","? )+ ")" | "(" ")" )`
}

type Arg struct {
    Name string `@Ident "="`
	Value *Value `@@`
}

type Dict struct {
    DictElements []*DictElement `( "{" ( @@ ","? )+ "}" | "{}" )`
}

type DictElement struct {
    Name string `@Text ":"`
    Value *Value `@@ ","?`
}

type List struct {
    ListElement []*ListElement `( "[" ( @@ ","? )+ "]" | "[" "]" )`
}

type ListElement struct {
    Value *Value `@@`
}

type Value struct {
    String *string `@Text`
    Dict *Dict `| @@`
    List *List `| @@`
}

func (v Value) Repr() string {
    if v.String != nil {
        return fmt.Sprintf("%v [string]", *(v.String))
    }
    if v.Dict != nil {
        return fmt.Sprintf("%v [dict]", *(v.Dict))
    }
    if v.List != nil {
        return fmt.Sprintf("%v [list]", *(v.List))
    }
    return "[no value]"
}
