package pyast

type Pants struct {
	PythonSources *PythonSources `@@`
}

type PythonSources struct {
	Overrides *Overrides `"python_sources(" @Ident ")"`
}

type Overrides struct {
	Target Target `"overrides={" @Ident "},"`
}

type Target struct {
	Name string `"\"" @Ident "\":`
}
