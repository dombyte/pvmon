package util

import (
	"fmt"
	"reflect"
	"strings"
)

// DependencyError reports constructor dependencies that were nil.
type DependencyError struct {
	Component string
	Missing   []string
}

func (e *DependencyError) Error() string {
	return fmt.Sprintf("%s: missing dependencies: %s", e.Component, strings.Join(e.Missing, ", "))
}

// Dep names one constructor dependency for RequireAll.
type Dep struct {
	Name  string
	Value any
}

// D is shorthand for a Dep.
func D(name string, value any) Dep { return Dep{Name: name, Value: value} }

// RequireAll returns a *DependencyError listing every dependency that is nil, including
// typed nil pointers, maps, funcs, channels and interfaces stored in an interface.
func RequireAll(component string, deps ...Dep) error {
	var missing []string
	for _, d := range deps {
		if isNil(d.Value) {
			missing = append(missing, d.Name)
		}
	}
	if len(missing) == 0 {
		return nil
	}
	return &DependencyError{Component: component, Missing: missing}
}

func isNil(v any) bool {
	if v == nil {
		return true
	}
	rv := reflect.ValueOf(v)
	switch rv.Kind() {
	case reflect.Pointer, reflect.Map, reflect.Func, reflect.Chan, reflect.Interface,
		reflect.Slice:
		return rv.IsNil()
	default:
		return false
	}
}
