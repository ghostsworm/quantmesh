package main

import (
	"reflect"

	"quantmesh/strategy"
)

// Check before invoking either private accounting proof or inventory methods.
// A non-nil interface can still contain a nil receiver.
func capitalReleaseStrategyMissing(current strategy.Strategy) bool {
	if current == nil {
		return true
	}
	value := reflect.ValueOf(current)
	switch value.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Ptr, reflect.Slice:
		return value.IsNil()
	default:
		return false
	}
}
