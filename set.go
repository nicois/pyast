package pyast

import "reflect"

type (
	Void    struct{}
	Classes map[string]Void
)

var Member Void

func CreateClasses(strings ...string) Classes {
	result := make(Classes)
	for _, s := range strings {
		result[s] = Member
	}
	return result
}

func (c Classes) Lister() []string {
	result := []string{}
	for class := range c {
		result = append(result, class)
	}
	return result
}

func (c Classes) SameAs(o Classes) bool {
	return reflect.DeepEqual(c, o)
}

func (p *Classes) Add(items ...string) *Classes {
	for _, i := range items {
		(*p)[i] = Member
	}
	return p
}

func (p *Classes) Discard(other Classes) *Classes {
	for o := range other {
		delete(*p, o)
	}
	return p
}

func (p *Classes) Union(other Classes) *Classes {
	for o := range other {
		(*p)[o] = Member
	}
	return p
}

func (p *Classes) Intersection(other Classes) *Classes {
	for mine := range *p {
		if _, exists := other[mine]; !exists {
			delete(*p, mine)
		}
	}
	return p
}

func (s *Classes) Copy() Classes {
	result := make(Classes)
	for o := range *s {
		result[o] = Member
	}
	return result
}
