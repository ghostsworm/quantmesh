package config

import (
	"reflect"
	"testing"
)

func TestStrategySnapshotPreservesTypesAndNestedIsolation(t *testing.T) {
	src := []StrategyInstance{{Type: "combo", Config: map[string]interface{}{
		"integer": int64(7), "unsigned": uint64(1<<63 + 3), "fraction": float64(2.5),
		"typed":    map[string][]int{"levels": {1, 2}},
		"children": []interface{}{map[string]interface{}{"weight": float64(0.5)}},
		"array":    [1][]int{{3}}, "nil": nil,
	}}}
	copy := CloneStrategyInstances(src)
	if !reflect.DeepEqual(copy, src) {
		t.Fatal("strategy snapshot changed values or concrete numeric types")
	}
	src[0].Config["typed"].(map[string][]int)["levels"][0] = 99
	src[0].Config["children"].([]interface{})[0].(map[string]interface{})["weight"] = float64(99)
	src[0].Config["array"].([1][]int)[0][0] = 99
	if copy[0].Config["typed"].(map[string][]int)["levels"][0] != 1 || copy[0].Config["children"].([]interface{})[0].(map[string]interface{})["weight"] != float64(0.5) || copy[0].Config["array"].([1][]int)[0][0] != 3 {
		t.Fatal("strategy container snapshot retained external aliases")
	}
}

func TestStrategySnapshotPreservesNilAndEmptyContainers(t *testing.T) {
	for _, src := range [][]StrategyInstance{nil, {}, {{Type: "grid"}}, {{Type: "grid", Config: map[string]interface{}{"nil_list": []int(nil), "empty_list": []int{}, "nil_map": map[string]interface{}(nil), "empty_map": map[string]interface{}{}}}}} {
		if !reflect.DeepEqual(CloneStrategyInstances(src), src) {
			t.Fatal("strategy snapshot changed nil/empty semantics")
		}
	}
}

func TestStrategySnapshotDoesNotExpandSharedGraph(t *testing.T) {
	shared := map[string]interface{}{"value": int(1)}
	src := []StrategyInstance{{Config: map[string]interface{}{"left": shared, "right": shared}}}
	copy := CloneStrategyInstances(src)
	left := copy[0].Config["left"].(map[string]interface{})
	right := copy[0].Config["right"].(map[string]interface{})
	if reflect.ValueOf(left).UnsafePointer() != reflect.ValueOf(right).UnsafePointer() {
		t.Fatal("shared strategy graph was expanded into duplicate containers")
	}
	left["value"] = int(2)
	if shared["value"] != int(1) {
		t.Fatal("graph cache reused caller-owned container")
	}
}

func TestStrategySnapshotFinishesOnCyclicMapAndSlice(t *testing.T) {
	src := map[string]interface{}{}
	src["self"] = src
	list := make([]interface{}, 1)
	list[0] = list
	src["list"] = list
	copy := CloneStrategyInstances([]StrategyInstance{{Config: src}})[0].Config
	if reflect.ValueOf(copy).UnsafePointer() == reflect.ValueOf(src).UnsafePointer() || reflect.ValueOf(copy["self"]).UnsafePointer() != reflect.ValueOf(copy).UnsafePointer() {
		t.Fatal("cyclic map graph not copied independently")
	}
	copiedList := copy["list"].([]interface{})
	if reflect.ValueOf(copiedList).UnsafePointer() == reflect.ValueOf(list).UnsafePointer() || reflect.ValueOf(copiedList[0]).UnsafePointer() != reflect.ValueOf(copiedList).UnsafePointer() {
		t.Fatal("cyclic slice graph not copied independently")
	}
}

func TestStrategySnapshotDistinctSliceViewsKeepTheirLength(t *testing.T) {
	values := []int{1, 2, 3}
	copy := CloneStrategyInstances([]StrategyInstance{{Config: map[string]interface{}{"short": values[:1], "long": values[:3]}}})[0].Config
	if len(copy["short"].([]int)) != 1 || len(copy["long"].([]int)) != 3 {
		t.Fatal("container cache confused slice views")
	}
}
