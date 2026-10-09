package config

import (
	"reflect"
	"unsafe"
)

type strategySnapshotReference struct {
	typeOf  reflect.Type
	address unsafe.Pointer
	length  int
}

// CloneStrategyInstances isolates declarative JSON/YAML strategy data while
// preserving numeric types (a JSON round-trip would turn integers into floats).
// Container identity is cached to avoid expanding shared graphs and to finish
// on cyclic containers. This is not validation of a runnable strategy config.
// The source must be stable, not arbitrary program objects or concurrently mutated maps.
func CloneStrategyInstances(src []StrategyInstance) []StrategyInstance {
	if src == nil {
		return nil
	}
	dst := make([]StrategyInstance, len(src))
	copy(dst, src)
	seen := make(map[strategySnapshotReference]reflect.Value)
	for i := range dst {
		dst[i].Config = cloneStrategyData(reflect.ValueOf(src[i].Config), seen).Interface().(map[string]interface{})
	}
	return dst
}

func cloneStrategyData(src reflect.Value, seen map[strategySnapshotReference]reflect.Value) reflect.Value {
	switch src.Kind() {
	case reflect.Interface:
		if src.IsNil() {
			return reflect.Zero(src.Type())
		}
		dst := reflect.New(src.Type()).Elem()
		dst.Set(cloneStrategyData(src.Elem(), seen))
		return dst
	case reflect.Map:
		if src.IsNil() {
			return reflect.Zero(src.Type())
		}
		key := strategySnapshotReference{typeOf: src.Type(), address: src.UnsafePointer()}
		if existing, ok := seen[key]; ok {
			return existing
		}
		dst := reflect.MakeMapWithSize(src.Type(), src.Len())
		seen[key] = dst
		iter := src.MapRange()
		for iter.Next() {
			dst.SetMapIndex(iter.Key(), cloneStrategyData(iter.Value(), seen))
		}
		return dst
	case reflect.Slice, reflect.Array:
		if src.Kind() == reflect.Slice && src.IsNil() {
			return reflect.Zero(src.Type())
		}
		dst := reflect.New(src.Type()).Elem()
		if src.Kind() == reflect.Slice {
			key := strategySnapshotReference{typeOf: src.Type(), address: src.UnsafePointer(), length: src.Len()}
			if existing, ok := seen[key]; ok {
				return existing
			}
			dst = reflect.MakeSlice(src.Type(), src.Len(), src.Len())
			seen[key] = dst
		}
		for i := range src.Len() {
			dst.Index(i).Set(cloneStrategyData(src.Index(i), seen))
		}
		return dst
	default:
		return src // declarative scalar values are immutable
	}
}
